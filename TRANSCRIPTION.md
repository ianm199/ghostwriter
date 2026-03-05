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

Three things made this worse for us:

1. **`base` model (74M params)** — too small to reliably distinguish speech from non-speech on long audio. The single biggest factor.
2. **No context break between segments** — by default, whisper conditions each 30-second window on the previous window's output. One hallucinated segment contaminates every subsequent segment.
3. **No VAD preprocessing** — silence windows (pauses between speakers, muted periods) are fed directly to the model, which is exactly where hallucination starts.

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

### Layer 2: Anti-Hallucination Flags

The two most impactful whisper-cli flags:

| Flag | Why |
|------|-----|
| `--max-context 0` | Prevents hallucination cascade. Each 30s window starts fresh instead of conditioning on potentially-hallucinated previous output. |
| `--temperature-inc 0.0` | Disables temperature fallback. When initial decoding fails quality checks, whisper normally retries at higher temperature — which produces even worse hallucinations. |

Full recommended flag set:

```bash
whisper-cli \
  -m ggml-large-v3-turbo-q5_0.bin \
  -f meeting.wav \
  --language en \
  --beam-size 5 \
  --max-context 0 \
  --temperature 0.0 \
  --temperature-inc 0.0 \
  --entropy-thold 2.4 \
  --logprob-thold -1.0 \
  --no-speech-thold 0.6 \
  --split-on-word
```

### Layer 3: VAD Preprocessing

whisper.cpp now has built-in Silero VAD support. This strips silence before transcription, removing the primary hallucination trigger entirely.

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

### Safety Net: Post-Processing

As a last resort, detect and remove hallucinated segments after transcription:

- **N-gram repetition**: if any 3-word sequence appears more than 3 times in a segment, flag it
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

### Phase 1: Fix the Obvious (model + flags)

- [ ] Download `large-v3-turbo-q5_0` model via `ghostwriter models download`
- [ ] Update `WhisperTranscriber` to pass anti-hallucination flags: `--max-context 0`, `--temperature-inc 0.0`, `--beam-size 5`, `--split-on-word`
- [ ] Make model selection configurable (currently hardcoded to `base`)
- [ ] Re-transcribe the 2026-03-04 recording to verify improvement

### Phase 2: VAD Integration

- [ ] Download Silero VAD ONNX model and bundle with model management
- [ ] Add `--vad` flags to whisper-cli invocation
- [ ] Test on meeting audio with long silence stretches

### Phase 3: Save Audio Option

- [ ] Add `save_audio` option to daemon config
- [ ] Copy WAV to output dir alongside `.transcript.json` before cleanup
- [ ] Enables re-transcription with different models for comparison

### Phase 4: Post-Processing Safety Net

- [ ] Add n-gram repetition detection to transcript pipeline
- [ ] Add compression ratio check
- [ ] Flag or remove hallucinated segments before writing final JSON

### Phase 5: Evaluate Parakeet

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
