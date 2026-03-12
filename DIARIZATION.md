# Speaker Diarization: Current State & Decision Point

## Problem

We want local/offline speaker diarization (who spoke when) for Ghostwriter transcripts. The whisper backend produces segments with no speaker info. AssemblyAI already does diarization via their API, but there's no local option.

## Architecture

```
pkg/diarize/          - sherpa-onnx wrapper + post-processing
pkg/transcribe/       - DiarizingTranscriber wraps any Transcriber, runs diarize as post-process
                        merges speaker labels into whisper segments by temporal overlap
```

**sherpa-onnx** provides Go bindings for offline diarization using:
- **Segmentation model**: pyannote-segmentation-3.0 ONNX (~6MB) — detects who's talking when in 10s sliding windows
- **Embedding model**: 3dspeaker eres2net (~28MB) — extracts speaker identity vectors
- **FastClustering**: groups embeddings into speaker clusters

The pipeline: segmentation → embedding extraction → clustering → labeled segments.

## The Clustering Problem

sherpa-onnx uses **agglomerative hierarchical clustering with complete linkage** on cosine dissimilarity. You set either `NumClusters` (if you know how many speakers) or `Threshold` (auto-detect).

**Complete linkage** means: two clusters merge only if the *most dissimilar pair* across them is below the threshold. This is pessimistic — a single outlier embedding prevents a merge. Over long recordings, speaker embeddings drift (energy, pitch, room acoustics change), so the same speaker's embeddings spread out and complete linkage fragments them.

**What pyannote's real pipeline uses instead**: centroid linkage with `min_cluster_size=12` and a tuned threshold of 0.7045. Centroid linkage compares cluster *averages*, which is much more stable. sherpa-onnx doesn't expose this option.

### Results with threshold-only approach (no post-processing)

| Threshold | 60s clip (4 speakers) | 3min clip | 21min full file |
|-----------|----------------------|-----------|-----------------|
| 0.5       | 13 speakers          | ~20       | 50+             |
| 0.85      | 4 speakers           | ~12       | 36 speakers     |
| 0.9       | 3 speakers           | ~9        | ~25             |

Short recordings work fine at any threshold. Long recordings always over-segment because complete linkage can't handle embedding drift.

## What We Tried (Post-Processing on sherpa-onnx Output)

### 1. Threshold tuning alone
Works for short recordings, fails for long ones. No single threshold works across recording lengths.

### 2. MinDurationOn / MinDurationOff
sherpa-onnx post-clustering filters (`MinDurationOn=0.3s`, `MinDurationOff=0.5s`). Helps reduce segment count but doesn't fix identity fragmentation.

### 3. Centroid-based post-merge (single-pass, union-find)
Re-extract embeddings via standalone `SpeakerEmbeddingExtractor`, average per speaker to get centroids, merge speakers above a cosine similarity threshold. Fixed threshold doesn't adapt — 0.6 too conservative, 0.5 collapses everything into one blob. Tiny speakers with noisy centroids poison the merge.

### 4. Adaptive gap-based threshold
Sort all pairwise centroid similarities, find the largest gap, threshold at the midpoint. Doesn't work — similarity distribution is continuous (0.3–0.7), not bimodal. No clean gap to exploit.

### 5. Iterative centroid merge with recomputation
Iteratively merge the most similar centroid pair, recompute the weighted-average centroid, repeat. Cascading merges do help — a merged centroid can attract speakers that neither original was individually close to. But the **stopping criterion is the hard part**:
- Fixed floor (0.35): over-merges short recordings
- Relative drop >30%: works for 60s (stops correctly at 3 speakers) but on 3min it never triggers and over-merges to 2

### Diagnosis

We are spending too much effort on stopping criteria for a merge process that starts from a bad graph. The core issue is structural: sherpa-onnx's complete linkage is biased toward over-splitting long recordings, and no amount of post-processing on the output segments fully recovers from that. The similarity distribution between same-speaker centroids (0.3–0.7) overlaps heavily with different-speaker centroids (0.3–0.5), making any threshold-based approach fragile.

## External Review Findings

### The recommended Go/ONNX path (if staying local, no Python)

A 3-stage long-audio cleanup pipeline:

**Stage A — Classify clusters as stable vs tiny/noisy.**
For each sherpa speaker cluster: compute total voiced duration, segment count, centroid, within-cluster spread. Stable if total speech >= 6-10s or at least 3 segments >= 1.0s each. Everything else is provisional. This prevents noisy centroids from poisoning global merge decisions.

**Stage B — Estimate speaker count from stable clusters only.**
Build an affinity matrix among stable clusters. Use NME-SC / eigengap on this reduced matrix to determine speaker count. This is the gold standard approach (NeMo's [Auto-Tuning Spectral Clustering](https://github.com/tango4j/Auto-Tuning-Spectral-Clustering)). Operating on 5-10 stable clusters instead of 30+ raw clusters makes it practical.

**Stage C — Reassign tiny clusters.**
Attach tiny clusters to the nearest stable speaker by centroid similarity + temporal compatibility. Don't let tiny clusters participate in global merge decisions.

**Critical missing ingredient: temporal constraints.**
- No merge if two clusters frequently overlap in time (same person can't talk at the same time as themselves)
- Prefer merges with turn-taking compatibility
- Discount centroids from acoustically dissimilar contexts

### The alternative: pyannote as a Python sidecar

[pyannote speaker-diarization-community-1](https://huggingface.co/pyannote/speaker-diarization-community-1) is the current best open-source diarization pipeline. It uses centroid linkage, proper `min_cluster_size`, and a tuned threshold — exactly what sherpa-onnx's ONNX export strips out.

If we can tolerate a Python process, this is the most obvious baseline to beat. It's not just "another wrapper" — it's a materially different pipeline than what sherpa-onnx exposes.

Other options considered:
- **NeMo Sortformer/MSDD**: Serious alternative, but Python-heavy and NeMo-config-oriented. Better as a benchmark than an embed target.
- **WhisperX**: Just wraps pyannote. Not a distinct diarization engine.
- **SpeechBrain**: Good reference for spectral clustering implementation, not a turnkey solution.
- **diart**: For streaming/incremental diarization. Relevant if we want near-real-time labels during recording, not for batch.

## Decision Point

### Option A: Stay in Go/ONNX, implement 3-stage pipeline

Implement stable/tiny classification → eigengap on stable clusters → reassign tiny. Add temporal overlap constraints. This is ~200-400 lines of Go, no new dependencies. Keeps everything local and fast for short recordings.

**Pro**: No Python dependency, works with existing sherpa-onnx setup, fast for short recordings.
**Con**: Significant implementation effort, may still be limited by embedding model quality.

### Option B: Add pyannote as a Python sidecar backend

Ship a small Python script that runs pyannote Community-1. Call it from Go the same way we call whisper-cli. Only invoked for long recordings or when `--diarize` is passed.

**Pro**: Best available diarization quality. Proven pipeline. Minimal custom clustering code.
**Con**: Python dependency (~2GB with torch). Harder to install. Two runtimes.

### Option C: Hybrid — sherpa for short, pyannote sidecar for long

Use sherpa-onnx directly for recordings < 2-3 minutes (works well today). For longer recordings, shell out to pyannote if available, fall back to sherpa with best-effort post-processing.

**Pro**: Best of both worlds. Graceful degradation.
**Con**: Most complex to maintain. Two code paths.

## Current Code State

- `pkg/diarize/diarize.go`: sherpa-onnx wrapper with iterative centroid merge (experimental, has debug output)
- `pkg/diarize/models.go`: Model download for segmentation + embedding ONNX files
- `pkg/transcribe/diarizing.go`: DiarizingTranscriber wrapper + transcript merge logic
- `pkg/transcribe/transcriber.go`: `Diarize bool` + `DiarizeConfig` on TranscriberConfig
- `internal/cli/transcribe.go`, `daemon.go`: `--diarize` flag wired through
- `internal/cli/models.go`: `models download diarize` and `models list` show diarization models
- `cmd/diarize-test/main.go`: Test program with `-diarize-only` flag

## Test Setup

- **Fixture**: AMI ES2002a.Mix-Headset.wav — 21min meeting, 4 speakers, mono 16kHz
- **Clips**: 60s and 3min extracts for fast iteration
- **Iteration time**: ~12s for 60s clip, ~58s for 3min clip, ~7min for full file
