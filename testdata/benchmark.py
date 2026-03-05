#!/usr/bin/env python3
import json
import sys
from jiwer import wer, process_words

def normalize(text):
    return text.upper().strip()

def load_whisper_json(path):
    with open(path) as f:
        data = json.load(f)
    segments = []
    for seg in data.get("transcription", []):
        text = seg.get("text", "").strip()
        if text:
            segments.append(text)
    return " ".join(segments)

def count_repetitions(text, n=3, threshold=3):
    words = text.split()
    ngrams = {}
    for i in range(len(words) - n + 1):
        gram = " ".join(words[i:i+n])
        ngrams[gram] = ngrams.get(gram, 0) + 1
    repeated = {k: v for k, v in ngrams.items() if v > threshold}
    return repeated

def main():
    if len(sys.argv) < 2:
        print("Usage: benchmark.py <whisper_output.json> [reference.txt]")
        sys.exit(1)

    hyp_path = sys.argv[1]
    ref_path = sys.argv[2] if len(sys.argv) > 2 else None

    hypothesis = load_whisper_json(hyp_path)
    hyp_norm = normalize(hypothesis)
    hyp_words = len(hyp_norm.split())

    print(f"=== {hyp_path} ===")
    print(f"Hypothesis words: {hyp_words}")
    print(f"First 200 chars: {hypothesis[:200]}")
    print()

    repetitions = count_repetitions(hyp_norm)
    if repetitions:
        print(f"HALLUCINATION DETECTED: {len(repetitions)} repeated 3-grams")
        top = sorted(repetitions.items(), key=lambda x: -x[1])[:5]
        for gram, count in top:
            print(f"  '{gram}' x{count}")
    else:
        print("No repetition loops detected")
    print()

    if ref_path:
        with open(ref_path) as f:
            reference = normalize(f.read())
        ref_words = len(reference.split())

        error_rate = wer(reference, hyp_norm)
        result = process_words(reference, hyp_norm)

        print(f"Reference words: {ref_words}")
        print(f"WER: {error_rate:.1%}")
        print(f"  Substitutions: {result.substitutions}")
        print(f"  Insertions: {result.insertions}")
        print(f"  Deletions: {result.deletions}")
        print(f"  Hits: {result.hits}")

if __name__ == "__main__":
    main()
