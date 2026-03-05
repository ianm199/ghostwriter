# Transcription Quality: Research & Improvement Plan

## The Problem

On 2026-03-04, the daemon successfully recorded a 14-minute Google Meet call via SCKit and transcribed it with `whisper-base.en`. The pipeline worked end-to-end (detection → capture → transcribe → save), but the transcript quality was poor. After a few segments of real content, the model entered a repetition loop, outputting "I will work with you as well" hundreds of times for the remainder of the file.

```json
{
  "duration_seconds": 862,
  "segments": 46,
  "model": "base",
  "full_text": "...So, that's the goal and today I will work with yourself. I will work with you as well. I will work with you as well. I will work with you as well. I will work with you as well..."
}
```

This is a well-documented Whisper failure mode. It's solvable.

---

## Root Cause

Whisper is autoregressive — it generates one token at a time, conditioned on previous tokens. When the acoustic signal is weak (silence, background noise, low volume), the model has nothing to ground its predictions on. It falls back to its language model prior and latches onto whatever it just said, creating a self-reinforcing repetition loop.

Four things made this worse for us:

1. **`base` model (74M params)** — too small to reliably distinguish speech from non-speech on long audio. The single biggest factor.
2. **No context break between segments** — by default, whisper conditions each 30-second window on the previous window's output. One hallucinated segment contaminates every subsequent segment.
3. **No VAD preprocessing** — silence windows (pauses between speakers, muted periods) are fed directly to the model, which is exactly where hallucination starts.
4. **No audio normalization** — system audio capture can produce low-amplitude, weird-stereo signals (OS mixer levels, Meet AGC, phase cancellation in downmix). Whisper sees a weak acoustic signal and freewheels into its language model prior.

---

## Fix: Three Layers

### Layer 1: Upgrade the Model

Switch from `base` (74M, 142 MiB) to `large-v3-turbo-q5_0` (809M, 547 MiB).

- Nearly matches large-v3 accuracy (WER ~7.75% vs ~7.4%)
- 6x faster than large-v3 thanks to a 4-layer decoder (vs 32)
- Quantized variant (q5_0) cuts disk/memory to 547 MiB with negligible quality loss
- Processes 15 minutes of audio in ~1-2 minutes on Apple Silicon with Metal

This alone eliminates ~90% of hallucination for English meeting audio.

```bash
ghostwriter models download large-v3-turbo-q5_0
ghostwriter config set transcription.model large-v3-turbo-q5_0
```

### Layer 2: Audio Normalization

Before whisper sees audio, normalize it. System audio capture produces inconsistent levels (OS mixer, Meet AGC, stereo downmix artifacts). A basic RMS normalize + proper mono downmix reduces the "weak signal" condition that starts the loop.

### Layer 3: Anti-Hallucination Flags

| Flag | Why |
|------|-----|
| `--max-context 0` | Prevents hallucination cascade between 30s windows. |
| `--beam-size 5` | Better decoding quality than greedy (default). |
| `--no-speech-thold 0.5` | Tightened from 0.6 — more aggressive at classifying silence as non-speech. Keep this even with VAD as a second guardrail, especially for low-level system audio noise. |
| `--temperature 0.0` | Deterministic initial decoding. |
| `--temperature-inc 0.2` | Allow one small fallback step rather than disabling entirely. The entropy/logprob thresholds are tied to fallback behavior — disabling it removes an escape hatch. The post-processing safety net catches bad retries anyway. |

```bash
whisper-cli \
  -m ggml-large-v3-turbo-q5_0.bin \
  -f meeting.wav \
  --language en \
  --beam-size 5 \
  --max-context 0 \
  --temperature 0.0 \
  --temperature-inc 0.2 \
  --entropy-thold 2.4 \
  --logprob-thold -1.0 \
  --no-speech-thold 0.5 \
  --split-on-word
```

All of these should be config-driven so the daemon can evolve without code changes.

### Layer 4: VAD Preprocessing

Two complementary approaches — use both:

1. **True VAD preprocessing** (Silero): cuts audio into speech-only spans before whisper sees it. This is the primary defense against silence-triggered hallucination.
2. **In-decoder no-speech gating** (`--no-speech-thold`): skips segments where whisper predicts high no-speech probability. Catches residual noise that VAD passes through.

```bash
whisper-cli \
  ... \
  --vad \
  --vad-model silero-vad.onnx \
  --vad-thold 0.5 \
  --vad-min-speech-duration-ms 250 \
  --vad-min-silence-duration-ms 500
```

The Silero VAD model is tiny (<2 MB) and runs in <1ms per 32ms audio chunk on CPU.

### Layer 5: Post-Processing Safety Net

Detect and remove hallucinated segments after transcription:

- **N-gram repetition**: if any 3-word sequence appears more than 3 times in a segment, flag it. Can also run a rolling window during segmented transcription to detect the loop as it starts and re-run just that 30s chunk with stricter settings.
- **Compression ratio**: gzip the segment text — if compression ratio > 2.4, it's repetitive
- **Confidence filtering**: drop segments where average log probability is below threshold

---

## Model Landscape

### Whisper Family (whisper.cpp)

| Model | Size | Hallucination Risk | Speed (Apple Silicon) | WER |
|-------|------|--------------------|-----------------------|-----|
| tiny.en | 75 MiB | Very High | ~10-15s/15min | ~12% |
| base.en | 142 MiB | **High (current)** | ~20-30s/15min | ~9% |
| small.en | 466 MiB | Moderate | ~45-60s/15min | ~7.5% |
| medium.en | 1.5 GiB | Low | ~2-3min/15min | ~7% |
| large-v3-turbo | 1.5 GiB | Low | ~1-2min/15min | ~7.75% |
| **large-v3-turbo-q5_0** | **547 MiB** | **Low** | **~1-2min/15min** | **~8%** |
| large-v3 | 2.9 GiB | Very Low | ~5-8min/15min | ~7.4% |

**Recommendation: `large-v3-turbo-q5_0`** — best balance of quality, speed, and memory for a background daemon.

### Non-Whisper Alternatives

| Model | Params | WER | C/C++ CLI | Apple Silicon | Notes |
|-------|--------|-----|-----------|---------------|-------|
| NVIDIA Parakeet TDT 0.6B | 600M | ~6.7% | parakeet.cpp (Metal) | Yes | #1 on HF leaderboard. Built-in diarization via sortformer. Needs chunking for >5min audio. |
| NVIDIA Canary-Qwen 2.5B | 2.5B | ~5.6% | No (Python/NeMo) | MPS fallback | Best accuracy but impractical for Go integration. |
| Moonshine | 27-62M | Moderate | moonshine.cpp | CPU only | Too small for meeting quality. Designed for voice commands. |
| Vosk | Various | Fair | C API + Go bindings | CPU | Native Go bindings but lower accuracy than whisper. |

**Watch: parakeet.cpp** — if it matures, it could replace whisper as the default backend. Better accuracy, native Metal, built-in speaker diarization. Currently limited to ~5min chunks (fine with VAD pre-segmentation).

---

## Test Audio Sources

For benchmarking transcription quality without needing a live call:

### Quick Start (single files)

- **AMI Meeting Corpus** — real 4-person design meetings, 20-35 min each. Download individual meetings in `headset-mix` format (single WAV combining all headset mics, closest to system audio capture): https://groups.inf.ed.ac.uk/ami/download/
- **Columbia Meeting Recorder** — 5-minute tabletop mic excerpt, 6 participants, 16kHz WAV, immediately usable: https://www.ee.columbia.edu/~dpwe/sounds/mr/
- **LibriSpeech test-clean** — 346 MB, clean read speech. Not meetings but useful for WER measurement against ground truth: https://www.openslr.org/12

### Full Corpora

- **AMI Meeting Corpus** — 100 hours of meetings with transcriptions: https://groups.inf.ed.ac.uk/ami/download/
- **ICSI Meeting Corpus** — 75 real research meetings from UC Berkeley: https://groups.inf.ed.ac.uk/ami/icsi/download/

### DIY

Record your own calls through the daemon. Add `save_audio` support so the raw WAV is kept alongside the transcript — then you can re-transcribe with different models/settings for A/B comparison.

---

## Implementation Plan

### Phase 0: Baseline Benchmark

Before changing anything, establish a reproducible benchmark:

- [ ] Download test audio with known transcripts (AMI meeting corpus or LibriSpeech test-clean)
- [ ] Add `save_audio` option so real captured WAVs are kept for re-transcription
- [ ] Run current pipeline (base model, no flags) against test audio, record WER and failure modes
- [ ] This is the baseline everything gets measured against

### Phase 1: Model + Flags + Normalization

- [ ] Download `large-v3-turbo-q5_0` model
- [ ] Add audio normalization (RMS normalize, proper mono downmix) before whisper sees the audio
- [ ] Update `WhisperTranscriber` to pass anti-hallucination flags: `--max-context 0`, `--temperature-inc 0.2`, `--beam-size 5`, `--no-speech-thold 0.5`, `--split-on-word`
- [ ] Make all transcription knobs config-driven (model, max_context, no_speech_thold, entropy_thold, logprob_thold, VAD thresholds)
- [ ] Re-run benchmark, compare WER and hallucination rate against Phase 0 baseline

### Phase 2: VAD Integration

- [ ] Download Silero VAD ONNX model and bundle with model management
- [ ] Add `--vad` flags to whisper-cli invocation
- [ ] Keep `--no-speech-thold` as a second guardrail for residual noise
- [ ] Re-run benchmark, compare against Phase 1

### Phase 3: Post-Processing Safety Net

- [ ] Add n-gram repetition detection (rolling window during segmented transcription — detect loops as they start, re-run that chunk with stricter settings)
- [ ] Add compression ratio check
- [ ] Confidence filtering on segment avg log probability
- [ ] Re-run benchmark, confirm no regressions

### Phase 4: Evaluate Parakeet

- [ ] Build parakeet.cpp on macOS
- [ ] Benchmark against whisper large-v3-turbo on the same test audio
- [ ] If quality/speed wins, add as a transcription backend alongside whisper
- [ ] Evaluate sortformer for speaker diarization (currently unimplemented)

---

## References

- whisper.cpp: https://github.com/ggml-org/whisper.cpp
- whisper.cpp hallucination discussion: https://github.com/ggml-org/whisper.cpp/discussions/1490
- Silero VAD: https://github.com/snakers4/silero-vad
- parakeet.cpp: https://github.com/Frikallo/parakeet.cpp
- WhisperX (VAD + alignment approach): https://arxiv.org/html/2303.00747v2
- faster-whisper: https://github.com/SYSTRAN/faster-whisper
- distil-whisper: https://github.com/huggingface/distil-whisper
- AMI Corpus: https://groups.inf.ed.ac.uk/ami/download/
- LibriSpeech: https://www.openslr.org/12
