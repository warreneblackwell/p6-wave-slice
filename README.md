# p6-wave-slice

A small, focused command-line utility for turning collections of WAV samples into evenly-sliced, P‑6‑ready files. It targets the [Roland P-6 Creative Sampler](https://www.roland.com/global/products/p-6/) **Chop** workflow, but works for any sampler that expects equal-length slices in a single WAV.

## Background

The Roland P-6 has a **Chop** feature in Sample Edit (Voice) mode that splits a sample into equal parts and assigns each slice to note numbers from C4 to D#9 (up to 64 slices). This is great for quickly auditioning and playing multiple samples from a single voice slot.

However, preparing samples for this workflow manually is tedious:
- Samples need matching sample rates and channel configurations
- Each slice needs to be the same duration
- Leading silence should be trimmed
- Files need to fit within the sampler's memory constraints

**p6-wave-slice** automates this. Point it at a folder, give it a search pattern like "kick" or "snare", and it will:
1. Recursively find all matching WAV files
2. Show you a summary of what was found
3. Batch-process them into combined WAV files ready for Chop mode

## What it does

```
Folder of samples
  ├─ kick_001.wav
  ├─ kick_002.wav
  ├─ kick_003.wav
  └─ ...
	│
	▼  normalize (rate/channels), trim silence, pad/trim
┌───────────────────────────────────────────────────────────┐
│ combined output: kick_32slices_batch001.wav               │
│ [slice1][slice2][slice3]...[slice32]                      │
└───────────────────────────────────────────────────────────┘
	│
	▼  P-6 Chop (C4 → D#9)
```

## Features

- **Recursive file search** with pattern matching (e.g., "kick" → finds all `*kick*.wav` files)
- **Automatic resampling** to target sample rate (44100, 22050, 14700, or 11025 Hz)
- **Channel conversion** (mono ↔ stereo)
- **Leading silence removal** — trims dead air at the start of samples
- **Automatic padding/truncation** — ensures each slice is exactly the right duration
- **Multiple format support** — PCM (8/16/24/32-bit), IEEE Float (32/64-bit), and Extensible WAV formats
- **Batch output** — creates multiple output files if you have more samples than slices

## Quick start

Build and run locally:

```bash
go build -o wavslice main.go
./wavslice -dir "/path/to/samples" -pattern "kick" -rate 44100 -slices 32 -output ./output
```

## Installation

```bash
go build -o wavslice main.go
```

## Prebuilt binaries

Prebuilt binaries are available in the dist folder for macOS (Intel and Apple Silicon), Linux (amd64 and arm64), and Windows (amd64). These are intended for users who don't have Go installed.

### Checksums (SHA-256)

Checksums are generated into dist/SHA256SUMS when you run:

```bash
make all
```

You can verify a downloaded binary with:

**macOS**
```bash
shasum -a 256 dist/wavslice-darwin-arm64
```

**Linux**
```bash
sha256sum dist/wavslice-linux-amd64
```

Compare the output to the corresponding entry in dist/SHA256SUMS.

## Usage

```bash
./wavslice -dir <directory> -pattern <search> -rate <hz> -slices <n> -output <dir> [-stereo]
```

### Arguments

| Flag | Description | Default |
|------|-------------|---------|
| `-dir` | Working directory to search for WAV files | `.` |
| `-pattern` | Search pattern (e.g., "kick", "snare", "hat") | *required* |
| `-rate` | Output sample rate in Hz (44100, 22050, 14700, 11025) | `44100` |
| `-slices` | Number of slices per output file (1-64) | `32` |
| `-output` | Output directory for combined WAV files | `.` |
| `-stereo` | Output stereo (default is mono) | `false` |

### Examples

**Find all kick samples and create 32-slice mono files at 44.1kHz:**
```bash
./wavslice -dir "/path/to/sample-library" -pattern "kick" -rate 44100 -slices 32 -output ./output
```

**Create 64-slice stereo snare compilation at 22.05kHz:**
```bash
./wavslice -dir "/path/to/samples" -pattern "snare" -rate 22050 -slices 64 -stereo -output ./output
```

**Quick 16-slice hihat pack:**
```bash
./wavslice -dir "./drums" -pattern "hat" -slices 16 -output ./output
```

### Output

The tool creates files named: `{pattern}_{slices}slices_batch{NNN}.wav`

For example: `kick_32slices_batch001.wav`, `kick_32slices_batch002.wav`, etc.

## Slice duration reference

Based on the SP-16's ~260,000 sample frame limit:

| Sample Rate | Channels | Max Duration | 32 Slices | 64 Slices |
|-------------|----------|--------------|-----------|-----------|
| 44.1 kHz | Mono | 5.9s | 184ms | 92ms |
| 22.05 kHz | Mono | 11.8s | 369ms | 184ms |
| 14.7 kHz | Mono | 17.8s | 556ms | 278ms |
| 11.025 kHz | Mono | 23.7s | 741ms | 370ms |
| 44.1 kHz | Stereo | 2.95s | 92ms | 46ms |
| 22.05 kHz | Stereo | 5.9s | 184ms | 92ms |

**Tip:** For kicks, 80ms+ is usually sufficient. For snares, aim for 120ms+. Use a lower sample rate or mono output if you need longer slice durations.

## Workflow

1. Run the tool with your search pattern
2. Review the summary of found files (sample rates, durations, channels)
3. Confirm to proceed
4. Transfer the output WAV files to your sampler
5. Load a file and use **Chop** mode to split it into slices
6. Play the slices from C4 upward!

## License

MIT
