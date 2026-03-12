#!/usr/bin/env python3.11
"""Speaker diarization via pyannote. Called by ghostwriter as a subprocess.

Install: pip install pyannote.audio torch

Usage: python3 pyannote-diarize.py <wav_path> [--num-speakers N] [--model MODEL]

Outputs JSON to stdout:
  {"segments": [{"start": 0.5, "end": 2.3, "speaker": 0}, ...], "num_speakers": 4}
"""

import argparse
import json
import os
import sys
import warnings

warnings.filterwarnings("ignore")


def main():
    parser = argparse.ArgumentParser(description="Speaker diarization via pyannote")
    parser.add_argument("wav_path", help="Path to WAV file")
    parser.add_argument("--num-speakers", type=int, default=0)
    parser.add_argument("--model", default="pyannote/speaker-diarization-3.1")
    args = parser.parse_args()

    try:
        import torch
        from pyannote.audio import Pipeline
    except ImportError:
        print(
            "pyannote.audio not installed. Run: pip install pyannote.audio torch",
            file=sys.stderr,
        )
        sys.exit(1)

    token = os.environ.get("HF_TOKEN")
    try:
        pipeline = Pipeline.from_pretrained(args.model, token=token)
    except Exception as e:
        print(f"Failed to load pipeline {args.model}: {e}", file=sys.stderr)
        if token is None:
            print(
                "Hint: pyannote models require a HuggingFace token. "
                "Accept terms at https://huggingface.co/pyannote/speaker-diarization-3.1 "
                "then set HF_TOKEN=hf_...",
                file=sys.stderr,
            )
        sys.exit(1)

    if torch.backends.mps.is_available():
        pipeline.to(torch.device("mps"))

    params = {}
    if args.num_speakers > 0:
        params["num_speakers"] = args.num_speakers

    result = pipeline(args.wav_path, **params)

    annotation = getattr(result, "speaker_diarization", result)

    segments = []
    speaker_map = {}
    next_id = 0

    for turn, _, speaker in annotation.itertracks(yield_label=True):
        if speaker not in speaker_map:
            speaker_map[speaker] = next_id
            next_id += 1
        segments.append(
            {
                "start": round(turn.start, 3),
                "end": round(turn.end, 3),
                "speaker": speaker_map[speaker],
            }
        )

    json.dump({"segments": segments, "num_speakers": len(speaker_map)}, sys.stdout)


if __name__ == "__main__":
    main()
