package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// ============================================================================
// formatSize tests
// ============================================================================

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}

	for _, tc := range tests {
		result := formatSize(tc.bytes)
		if result != tc.expected {
			t.Errorf("formatSize(%d) = %s, expected %s", tc.bytes, result, tc.expected)
		}
	}
}

// ============================================================================
// sanitizeFilename tests
// ============================================================================

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"kick", "kick"},
		{"snare_01", "snare_01"},
		{"test<file>", "test_file_"},
		{"file:name", "file_name"},
		{"path/to/file", "path_to_file"},
		{"file\"with\"quotes", "file_with_quotes"},
		{"pipe|test", "pipe_test"},
		{"question?mark", "question_mark"},
		{"star*pattern", "star_pattern"},
		{"normal-file.name", "normal-file.name"},
		{"", ""},
	}

	for _, tc := range tests {
		result := sanitizeFilename(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeFilename(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

// ============================================================================
// resample tests
// ============================================================================

func TestResample(t *testing.T) {
	t.Run("same rate returns input", func(t *testing.T) {
		samples := [][]float64{{0.1, 0.2, 0.3, 0.4}}
		out := resample(samples, 44100, 44100)
		if len(out[0]) != len(samples[0]) {
			t.Errorf("expected same length %d, got %d", len(samples[0]), len(out[0]))
		}
		for i := range samples[0] {
			if out[0][i] != samples[0][i] {
				t.Errorf("sample %d: expected %f, got %f", i, samples[0][i], out[0][i])
			}
		}
	})

	t.Run("downsample 2:1", func(t *testing.T) {
		samples := [][]float64{{0, 1, 0, -1}}
		out := resample(samples, 4, 2)
		if len(out[0]) != 2 {
			t.Fatalf("expected 2 samples, got %d", len(out[0]))
		}
		// First sample should be at index 0 (value 0)
		if out[0][0] != 0 {
			t.Errorf("expected first sample 0, got %f", out[0][0])
		}
	})

	t.Run("upsample 1:2", func(t *testing.T) {
		samples := [][]float64{{0, 1}}
		out := resample(samples, 1, 2)
		if len(out[0]) != 4 {
			t.Fatalf("expected 4 samples, got %d", len(out[0]))
		}
	})

	t.Run("stereo resample", func(t *testing.T) {
		samples := [][]float64{
			{0, 0.5, 1.0, 0.5},
			{1.0, 0.5, 0, 0.5},
		}
		out := resample(samples, 4, 2)
		if len(out) != 2 {
			t.Fatalf("expected 2 channels, got %d", len(out))
		}
		if len(out[0]) != 2 || len(out[1]) != 2 {
			t.Fatalf("expected 2 samples per channel")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		samples := [][]float64{{}}
		out := resample(samples, 44100, 22050)
		if len(out[0]) != 0 {
			t.Errorf("expected empty output, got %d samples", len(out[0]))
		}
	})
}

// ============================================================================
// convertChannels tests
// ============================================================================

func TestConvertChannels(t *testing.T) {
	t.Run("mono to stereo", func(t *testing.T) {
		mono := [][]float64{{0.5, -0.5, 0.25}}
		stereo := convertChannels(mono, 2)
		if len(stereo) != 2 {
			t.Fatalf("expected 2 channels, got %d", len(stereo))
		}
		for i := range mono[0] {
			if stereo[0][i] != mono[0][i] || stereo[1][i] != mono[0][i] {
				t.Errorf("sample %d not duplicated correctly", i)
			}
		}
	})

	t.Run("stereo to mono", func(t *testing.T) {
		stereo := [][]float64{{1.0, 0.5}, {0.0, 0.5}}
		mono := convertChannels(stereo, 1)
		if len(mono) != 1 {
			t.Fatalf("expected 1 channel, got %d", len(mono))
		}
		// Average of 1.0 and 0.0 = 0.5
		if math.Abs(mono[0][0]-0.5) > 1e-9 {
			t.Errorf("expected 0.5, got %f", mono[0][0])
		}
		// Average of 0.5 and 0.5 = 0.5
		if math.Abs(mono[0][1]-0.5) > 1e-9 {
			t.Errorf("expected 0.5, got %f", mono[0][1])
		}
	})

	t.Run("stereo cancellation to mono", func(t *testing.T) {
		stereo := [][]float64{{1, 0}, {-1, 0}}
		mono := convertChannels(stereo, 1)
		// Average of 1 and -1 = 0
		if math.Abs(mono[0][0]) > 1e-9 {
			t.Errorf("expected 0, got %f", mono[0][0])
		}
	})

	t.Run("same channel count", func(t *testing.T) {
		samples := [][]float64{{0.1, 0.2}, {0.3, 0.4}}
		out := convertChannels(samples, 2)
		if len(out) != 2 {
			t.Fatalf("expected 2 channels, got %d", len(out))
		}
		// Should return same data
		for ch := range samples {
			for i := range samples[ch] {
				if out[ch][i] != samples[ch][i] {
					t.Errorf("data changed unexpectedly")
				}
			}
		}
	})

	t.Run("multichannel to mono", func(t *testing.T) {
		multi := [][]float64{{1.0}, {2.0}, {3.0}, {4.0}}
		mono := convertChannels(multi, 1)
		// Average = (1+2+3+4)/4 = 2.5
		if math.Abs(mono[0][0]-2.5) > 1e-9 {
			t.Errorf("expected 2.5, got %f", mono[0][0])
		}
	})

	t.Run("mono to more channels pads with zeros", func(t *testing.T) {
		mono := [][]float64{{0.5, 0.5}}
		out := convertChannels(mono, 4)
		if len(out) != 4 {
			t.Fatalf("expected 4 channels, got %d", len(out))
		}
		// First channel should have data
		if out[0][0] != 0.5 {
			t.Errorf("expected 0.5, got %f", out[0][0])
		}
		// Other channels should be zero-filled
		for ch := 1; ch < 4; ch++ {
			for i := range out[ch] {
				if out[ch][i] != 0 {
					t.Errorf("channel %d sample %d should be 0, got %f", ch, i, out[ch][i])
				}
			}
		}
	})
}

// ============================================================================
// removeLeadingSilence tests
// ============================================================================

func TestRemoveLeadingSilence(t *testing.T) {
	t.Run("basic silence removal", func(t *testing.T) {
		samples := [][]float64{{0, 0, 0.002, 1.0}}
		out := removeLeadingSilence(samples)
		if len(out[0]) != 2 {
			t.Fatalf("expected 2 samples after trim, got %d", len(out[0]))
		}
		if out[0][0] < 0.001 {
			t.Errorf("expected first sample above threshold, got %f", out[0][0])
		}
	})

	t.Run("no silence", func(t *testing.T) {
		samples := [][]float64{{0.5, 0.3, 0.1}}
		out := removeLeadingSilence(samples)
		if len(out[0]) != 3 {
			t.Fatalf("expected 3 samples, got %d", len(out[0]))
		}
	})

	t.Run("all silence", func(t *testing.T) {
		samples := [][]float64{{0, 0, 0, 0}}
		out := removeLeadingSilence(samples)
		if len(out[0]) != 1 {
			t.Fatalf("expected 1 sample for all-silent, got %d", len(out[0]))
		}
	})

	t.Run("stereo silence removal", func(t *testing.T) {
		samples := [][]float64{
			{0, 0, 0.5, 1.0},
			{0, 0, 0.3, 0.8},
		}
		out := removeLeadingSilence(samples)
		if len(out[0]) != 2 || len(out[1]) != 2 {
			t.Fatalf("expected 2 samples per channel")
		}
	})

	t.Run("empty samples", func(t *testing.T) {
		samples := [][]float64{}
		out := removeLeadingSilence(samples)
		if len(out) != 0 {
			t.Errorf("expected empty output")
		}
	})

	t.Run("empty channel", func(t *testing.T) {
		samples := [][]float64{{}}
		out := removeLeadingSilence(samples)
		if len(out[0]) != 0 {
			t.Errorf("expected empty channel")
		}
	})

	t.Run("threshold edge case", func(t *testing.T) {
		// Value exactly at threshold (0.001) should be considered silent
		samples := [][]float64{{0.001, 0.0011, 0.5}}
		out := removeLeadingSilence(samples)
		// 0.001 is at threshold, 0.0011 is above
		if len(out[0]) != 2 {
			t.Fatalf("expected 2 samples, got %d", len(out[0]))
		}
	})
}

// ============================================================================
// padOrTruncate tests
// ============================================================================

func TestPadOrTruncate(t *testing.T) {
	t.Run("pad shorter samples", func(t *testing.T) {
		samples := [][]float64{{1, 2, 3}}
		out := padOrTruncate(samples, 5)
		if len(out[0]) != 5 {
			t.Fatalf("expected length 5, got %d", len(out[0]))
		}
		if out[0][0] != 1 || out[0][1] != 2 || out[0][2] != 3 {
			t.Error("original samples changed")
		}
		if out[0][3] != 0 || out[0][4] != 0 {
			t.Error("padding should be zeros")
		}
	})

	t.Run("truncate longer samples", func(t *testing.T) {
		samples := [][]float64{{1, 2, 3, 4, 5}}
		out := padOrTruncate(samples, 2)
		if len(out[0]) != 2 {
			t.Fatalf("expected length 2, got %d", len(out[0]))
		}
		if out[0][0] != 1 || out[0][1] != 2 {
			t.Error("truncated samples incorrect")
		}
	})

	t.Run("exact length unchanged", func(t *testing.T) {
		samples := [][]float64{{1, 2, 3}}
		out := padOrTruncate(samples, 3)
		if len(out[0]) != 3 {
			t.Fatalf("expected length 3, got %d", len(out[0]))
		}
	})

	t.Run("stereo pad", func(t *testing.T) {
		samples := [][]float64{{1, 2}, {3, 4}}
		out := padOrTruncate(samples, 4)
		if len(out) != 2 {
			t.Fatalf("expected 2 channels")
		}
		if len(out[0]) != 4 || len(out[1]) != 4 {
			t.Fatalf("expected length 4 per channel")
		}
	})

	t.Run("empty samples", func(t *testing.T) {
		samples := [][]float64{}
		out := padOrTruncate(samples, 5)
		if len(out) != 0 {
			t.Error("expected empty output")
		}
	})
}

// ============================================================================
// concatenateSamples tests
// ============================================================================

func TestConcatenateSamples(t *testing.T) {
	t.Run("basic concatenation", func(t *testing.T) {
		all := [][][]float64{{{1, 2}}, {{3}}}
		out := concatenateSamples(all, 1)
		if len(out[0]) != 3 {
			t.Fatalf("expected length 3, got %d", len(out[0]))
		}
		if out[0][0] != 1 || out[0][1] != 2 || out[0][2] != 3 {
			t.Error("concatenation incorrect")
		}
	})

	t.Run("stereo concatenation", func(t *testing.T) {
		all := [][][]float64{
			{{1, 2}, {3, 4}},
			{{5}, {6}},
		}
		out := concatenateSamples(all, 2)
		if len(out) != 2 {
			t.Fatalf("expected 2 channels")
		}
		if len(out[0]) != 3 || len(out[1]) != 3 {
			t.Fatalf("expected 3 samples per channel")
		}
		if out[0][0] != 1 || out[0][1] != 2 || out[0][2] != 5 {
			t.Error("channel 0 incorrect")
		}
		if out[1][0] != 3 || out[1][1] != 4 || out[1][2] != 6 {
			t.Error("channel 1 incorrect")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		all := [][][]float64{}
		out := concatenateSamples(all, 1)
		if len(out) != 1 {
			t.Fatalf("expected 1 channel")
		}
		if len(out[0]) != 0 {
			t.Error("expected empty samples")
		}
	})

	t.Run("mixed empty and non-empty", func(t *testing.T) {
		all := [][][]float64{{{1, 2}}, {}, {{3}}}
		out := concatenateSamples(all, 1)
		if len(out[0]) != 3 {
			t.Fatalf("expected 3 samples, got %d", len(out[0]))
		}
	})
}

// ============================================================================
// normalizeSamples tests
// ============================================================================

func TestNormalizeSamples(t *testing.T) {
	t.Run("basic normalization", func(t *testing.T) {
		samples := [][]float64{{0.5, -0.25}}
		out := normalizeSamples(samples)

		peak := 0.0
		for _, v := range out[0] {
			if math.Abs(v) > peak {
				peak = math.Abs(v)
			}
		}
		if math.Abs(peak-1.0) > 1e-9 {
			t.Errorf("expected peak 1.0, got %f", peak)
		}
	})

	t.Run("already normalized", func(t *testing.T) {
		samples := [][]float64{{1.0, -1.0, 0.5}}
		out := normalizeSamples(samples)
		if out[0][0] != 1.0 || out[0][1] != -1.0 {
			t.Error("already normalized samples should be unchanged")
		}
	})

	t.Run("all zeros", func(t *testing.T) {
		samples := [][]float64{{0, 0, 0}}
		out := normalizeSamples(samples)
		for _, v := range out[0] {
			if v != 0 {
				t.Error("zero samples should remain zero")
			}
		}
	})

	t.Run("stereo normalization", func(t *testing.T) {
		samples := [][]float64{{0.25, 0.5}, {0.1, 0.2}}
		out := normalizeSamples(samples)
		// Peak is 0.5, scale is 2.0
		if math.Abs(out[0][1]-1.0) > 1e-9 {
			t.Errorf("expected peak 1.0, got %f", out[0][1])
		}
	})

	t.Run("empty samples", func(t *testing.T) {
		samples := [][]float64{}
		out := normalizeSamples(samples)
		if len(out) != 0 {
			t.Error("expected empty output")
		}
	})

	t.Run("empty channel", func(t *testing.T) {
		samples := [][]float64{{}}
		out := normalizeSamples(samples)
		if len(out[0]) != 0 {
			t.Error("expected empty channel")
		}
	})

	t.Run("negative peak", func(t *testing.T) {
		samples := [][]float64{{0.1, -0.8, 0.2}}
		out := normalizeSamples(samples)
		// Peak is 0.8, scale is 1.25
		if math.Abs(out[0][1]+1.0) > 1e-9 {
			t.Errorf("expected -1.0, got %f", out[0][1])
		}
	})
}

// ============================================================================
// WAV header helper functions
// ============================================================================

func createTestWavBuffer(audioFormat uint16, bitsPerSample uint16, sampleRate uint32, numChannels uint16, samples []byte) *bytes.Buffer {
	buf := new(bytes.Buffer)

	bytesPerSample := bitsPerSample / 8
	blockAlign := numChannels * bytesPerSample
	byteRate := sampleRate * uint32(blockAlign)
	dataSize := uint32(len(samples))

	// RIFF header
	buf.Write([]byte("RIFF"))
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.Write([]byte("WAVE"))

	// fmt chunk
	buf.Write([]byte("fmt "))
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, audioFormat)
	binary.Write(buf, binary.LittleEndian, numChannels)
	binary.Write(buf, binary.LittleEndian, sampleRate)
	binary.Write(buf, binary.LittleEndian, byteRate)
	binary.Write(buf, binary.LittleEndian, blockAlign)
	binary.Write(buf, binary.LittleEndian, bitsPerSample)

	// data chunk
	buf.Write([]byte("data"))
	binary.Write(buf, binary.LittleEndian, dataSize)
	buf.Write(samples)

	return buf
}

func createExtensibleWavBuffer(subFormat [16]byte, bitsPerSample uint16, validBits uint16, sampleRate uint32, numChannels uint16, samples []byte) *bytes.Buffer {
	buf := new(bytes.Buffer)

	bytesPerSample := bitsPerSample / 8
	blockAlign := numChannels * bytesPerSample
	byteRate := sampleRate * uint32(blockAlign)
	dataSize := uint32(len(samples))

	// RIFF header
	buf.Write([]byte("RIFF"))
	binary.Write(buf, binary.LittleEndian, uint32(60+dataSize)) // 12 + 8 + 40 + 8 + data
	buf.Write([]byte("WAVE"))

	// fmt chunk (extensible = 40 bytes)
	buf.Write([]byte("fmt "))
	binary.Write(buf, binary.LittleEndian, uint32(40))
	binary.Write(buf, binary.LittleEndian, uint16(0xFFFE)) // extensible
	binary.Write(buf, binary.LittleEndian, numChannels)
	binary.Write(buf, binary.LittleEndian, sampleRate)
	binary.Write(buf, binary.LittleEndian, byteRate)
	binary.Write(buf, binary.LittleEndian, blockAlign)
	binary.Write(buf, binary.LittleEndian, bitsPerSample)
	binary.Write(buf, binary.LittleEndian, uint16(22)) // cbSize
	binary.Write(buf, binary.LittleEndian, validBits)
	binary.Write(buf, binary.LittleEndian, uint32(0)) // channel mask
	buf.Write(subFormat[:])

	// data chunk
	buf.Write([]byte("data"))
	binary.Write(buf, binary.LittleEndian, dataSize)
	buf.Write(samples)

	return buf
}

// ============================================================================
// readWavHeader tests
// ============================================================================

func TestReadWavHeader(t *testing.T) {
	t.Run("valid 16-bit PCM", func(t *testing.T) {
		samples := make([]byte, 8) // 4 samples * 2 bytes
		buf := createTestWavBuffer(1, 16, 44100, 1, samples)
		r := bytes.NewReader(buf.Bytes())

		header, dataSize, err := readWavHeader(r)
		if err != nil {
			t.Fatalf("readWavHeader failed: %v", err)
		}
		if header.AudioFormat != 1 {
			t.Errorf("expected AudioFormat 1, got %d", header.AudioFormat)
		}
		if header.SampleRate != 44100 {
			t.Errorf("expected SampleRate 44100, got %d", header.SampleRate)
		}
		if header.NumChannels != 1 {
			t.Errorf("expected NumChannels 1, got %d", header.NumChannels)
		}
		if header.BitsPerSample != 16 {
			t.Errorf("expected BitsPerSample 16, got %d", header.BitsPerSample)
		}
		if dataSize != 8 {
			t.Errorf("expected dataSize 8, got %d", dataSize)
		}
	})

	t.Run("valid stereo 24-bit PCM", func(t *testing.T) {
		samples := make([]byte, 12) // 2 samples * 2 channels * 3 bytes
		buf := createTestWavBuffer(1, 24, 48000, 2, samples)
		r := bytes.NewReader(buf.Bytes())

		header, dataSize, err := readWavHeader(r)
		if err != nil {
			t.Fatalf("readWavHeader failed: %v", err)
		}
		if header.NumChannels != 2 {
			t.Errorf("expected 2 channels, got %d", header.NumChannels)
		}
		if header.BitsPerSample != 24 {
			t.Errorf("expected 24-bit, got %d", header.BitsPerSample)
		}
		if dataSize != 12 {
			t.Errorf("expected dataSize 12, got %d", dataSize)
		}
	})

	t.Run("32-bit float", func(t *testing.T) {
		samples := make([]byte, 8) // 2 samples * 4 bytes
		buf := createTestWavBuffer(3, 32, 44100, 1, samples)
		r := bytes.NewReader(buf.Bytes())

		header, _, err := readWavHeader(r)
		if err != nil {
			t.Fatalf("readWavHeader failed: %v", err)
		}
		if header.AudioFormat != 3 {
			t.Errorf("expected AudioFormat 3 (float), got %d", header.AudioFormat)
		}
	})

	t.Run("extensible PCM format", func(t *testing.T) {
		samples := make([]byte, 4) // 2 samples * 2 bytes
		buf := createExtensibleWavBuffer(subFormatPCM, 16, 16, 44100, 1, samples)
		r := bytes.NewReader(buf.Bytes())

		header, dataSize, err := readWavHeader(r)
		if err != nil {
			t.Fatalf("readWavHeader failed: %v", err)
		}
		if header.AudioFormat != 0xFFFE {
			t.Errorf("expected extensible format 0xFFFE, got %d", header.AudioFormat)
		}
		if dataSize != 4 {
			t.Errorf("expected dataSize 4, got %d", dataSize)
		}
	})

	t.Run("extensible float format", func(t *testing.T) {
		samples := make([]byte, 8) // 2 samples * 4 bytes
		buf := createExtensibleWavBuffer(subFormatFloat, 32, 32, 44100, 1, samples)
		r := bytes.NewReader(buf.Bytes())

		header, _, err := readWavHeader(r)
		if err != nil {
			t.Fatalf("readWavHeader failed: %v", err)
		}
		if header.AudioFormat != 0xFFFE {
			t.Errorf("expected extensible format 0xFFFE, got %d", header.AudioFormat)
		}
		if header.ExtSubFormat != subFormatFloat {
			t.Error("expected float subformat")
		}
	})

	t.Run("invalid RIFF marker", func(t *testing.T) {
		buf := bytes.NewBuffer([]byte("XXXX"))
		r := bytes.NewReader(buf.Bytes())

		_, _, err := readWavHeader(r)
		if err == nil {
			t.Error("expected error for invalid RIFF marker")
		}
	})

	t.Run("invalid WAVE marker", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(100))
		buf.Write([]byte("XXXX"))
		r := bytes.NewReader(buf.Bytes())

		_, _, err := readWavHeader(r)
		if err == nil {
			t.Error("expected error for invalid WAVE marker")
		}
	})

	t.Run("missing fmt chunk", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(20))
		buf.Write([]byte("WAVE"))
		buf.Write([]byte("data"))
		binary.Write(buf, binary.LittleEndian, uint32(4))
		buf.Write([]byte{0, 0, 0, 0})
		r := bytes.NewReader(buf.Bytes())

		_, _, err := readWavHeader(r)
		if err == nil {
			t.Error("expected error for missing fmt chunk")
		}
	})

	t.Run("missing data chunk", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(24))
		buf.Write([]byte("WAVE"))
		buf.Write([]byte("fmt "))
		binary.Write(buf, binary.LittleEndian, uint32(16))
		binary.Write(buf, binary.LittleEndian, uint16(1))  // format
		binary.Write(buf, binary.LittleEndian, uint16(1))  // channels
		binary.Write(buf, binary.LittleEndian, uint32(44100))
		binary.Write(buf, binary.LittleEndian, uint32(88200))
		binary.Write(buf, binary.LittleEndian, uint16(2))
		binary.Write(buf, binary.LittleEndian, uint16(16))
		r := bytes.NewReader(buf.Bytes())

		_, _, err := readWavHeader(r)
		if err == nil {
			t.Error("expected error for missing data chunk")
		}
	})

	t.Run("skips unknown chunks", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(60))
		buf.Write([]byte("WAVE"))

		// fmt chunk
		buf.Write([]byte("fmt "))
		binary.Write(buf, binary.LittleEndian, uint32(16))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint32(44100))
		binary.Write(buf, binary.LittleEndian, uint32(88200))
		binary.Write(buf, binary.LittleEndian, uint16(2))
		binary.Write(buf, binary.LittleEndian, uint16(16))

		// Unknown chunk
		buf.Write([]byte("INFO"))
		binary.Write(buf, binary.LittleEndian, uint32(8))
		buf.Write([]byte("testdata"))

		// data chunk
		buf.Write([]byte("data"))
		binary.Write(buf, binary.LittleEndian, uint32(4))
		buf.Write([]byte{0, 0, 0, 0})

		r := bytes.NewReader(buf.Bytes())
		_, dataSize, err := readWavHeader(r)
		if err != nil {
			t.Fatalf("readWavHeader failed: %v", err)
		}
		if dataSize != 4 {
			t.Errorf("expected dataSize 4, got %d", dataSize)
		}
	})

	t.Run("invalid fmt chunk size", func(t *testing.T) {
		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(20))
		buf.Write([]byte("WAVE"))
		buf.Write([]byte("fmt "))
		binary.Write(buf, binary.LittleEndian, uint32(8)) // Too small
		buf.Write(make([]byte, 8))
		r := bytes.NewReader(buf.Bytes())

		_, _, err := readWavHeader(r)
		if err == nil {
			t.Error("expected error for invalid fmt chunk size")
		}
	})
}

// ============================================================================
// writeWavFile and readWavFile round-trip tests
// ============================================================================

func TestWriteAndReadWavFile(t *testing.T) {
	t.Run("mono 16-bit roundtrip", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.wav")

		samples := [][]float64{{0, 0.5, -0.5, 1.0}}
		if err := writeWavFile(path, samples, 44100, 1); err != nil {
			t.Fatalf("writeWavFile failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if wav.Header.SampleRate != 44100 {
			t.Errorf("expected sample rate 44100, got %d", wav.Header.SampleRate)
		}
		if wav.Header.NumChannels != 1 {
			t.Errorf("expected mono, got %d channels", wav.Header.NumChannels)
		}
		if len(wav.Samples[0]) != 4 {
			t.Errorf("expected 4 samples, got %d", len(wav.Samples[0]))
		}
	})

	t.Run("stereo roundtrip", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "stereo.wav")

		samples := [][]float64{
			{0.1, 0.2, 0.3},
			{0.4, 0.5, 0.6},
		}
		if err := writeWavFile(path, samples, 48000, 2); err != nil {
			t.Fatalf("writeWavFile failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if wav.Header.NumChannels != 2 {
			t.Errorf("expected stereo, got %d channels", wav.Header.NumChannels)
		}
		if len(wav.Samples) != 2 {
			t.Errorf("expected 2 channels of samples")
		}
	})

	t.Run("empty samples", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.wav")

		samples := [][]float64{}
		if err := writeWavFile(path, samples, 44100, 1); err != nil {
			t.Fatalf("writeWavFile failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for zero data size")
		}
	})

	t.Run("clipping values", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "clip.wav")

		samples := [][]float64{{2.0, -2.0}} // Outside [-1, 1]
		if err := writeWavFile(path, samples, 44100, 1); err != nil {
			t.Fatalf("writeWavFile failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		// Values should be clipped to [-1, 1]
		if wav.Samples[0][0] < 0.99 || wav.Samples[0][0] > 1.01 {
			t.Errorf("expected clipped value near 1.0, got %f", wav.Samples[0][0])
		}
		if wav.Samples[0][1] > -0.99 || wav.Samples[0][1] < -1.01 {
			t.Errorf("expected clipped value near -1.0, got %f", wav.Samples[0][1])
		}
	})
}

// ============================================================================
// readWavFile bit depth tests
// ============================================================================

func TestReadWavFileBitDepths(t *testing.T) {
	t.Run("8-bit PCM", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "8bit.wav")

		// Create 8-bit WAV manually (unsigned 0-255, 128=silence)
		samples := []byte{128, 255, 0, 192} // silence, max, min, mid-positive
		buf := createTestWavBuffer(1, 8, 44100, 1, samples)

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if len(wav.Samples[0]) != 4 {
			t.Fatalf("expected 4 samples, got %d", len(wav.Samples[0]))
		}
		// 128 -> 0, 255 -> ~1.0, 0 -> ~-1.0
		if math.Abs(wav.Samples[0][0]) > 0.01 {
			t.Errorf("expected ~0, got %f", wav.Samples[0][0])
		}
	})

	t.Run("24-bit PCM", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "24bit.wav")

		// Create 24-bit WAV manually
		samples := []byte{
			0x00, 0x00, 0x00, // 0
			0xFF, 0xFF, 0x7F, // max positive (8388607)
			0x00, 0x00, 0x80, // max negative (-8388608)
		}
		buf := createTestWavBuffer(1, 24, 44100, 1, samples)

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if len(wav.Samples[0]) != 3 {
			t.Fatalf("expected 3 samples, got %d", len(wav.Samples[0]))
		}
		if math.Abs(wav.Samples[0][0]) > 0.01 {
			t.Errorf("expected ~0, got %f", wav.Samples[0][0])
		}
	})

	t.Run("32-bit PCM", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "32bit.wav")

		samples := make([]byte, 8)
		binary.LittleEndian.PutUint32(samples[0:4], 0)           // 0
		binary.LittleEndian.PutUint32(samples[4:8], 0x7FFFFFFF)  // max positive

		buf := createTestWavBuffer(1, 32, 44100, 1, samples)

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if len(wav.Samples[0]) != 2 {
			t.Fatalf("expected 2 samples, got %d", len(wav.Samples[0]))
		}
	})

	t.Run("32-bit float", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "32float.wav")

		samples := make([]byte, 8)
		binary.LittleEndian.PutUint32(samples[0:4], math.Float32bits(0.5))
		binary.LittleEndian.PutUint32(samples[4:8], math.Float32bits(-0.5))

		buf := createTestWavBuffer(3, 32, 44100, 1, samples)

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if len(wav.Samples[0]) != 2 {
			t.Fatalf("expected 2 samples, got %d", len(wav.Samples[0]))
		}
		if math.Abs(wav.Samples[0][0]-0.5) > 0.01 {
			t.Errorf("expected 0.5, got %f", wav.Samples[0][0])
		}
		if math.Abs(wav.Samples[0][1]+0.5) > 0.01 {
			t.Errorf("expected -0.5, got %f", wav.Samples[0][1])
		}
	})

	t.Run("64-bit float", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "64float.wav")

		samples := make([]byte, 16)
		binary.LittleEndian.PutUint64(samples[0:8], math.Float64bits(0.75))
		binary.LittleEndian.PutUint64(samples[8:16], math.Float64bits(-0.25))

		buf := createTestWavBuffer(3, 64, 44100, 1, samples)

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		wav, err := readWavFile(path)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if len(wav.Samples[0]) != 2 {
			t.Fatalf("expected 2 samples, got %d", len(wav.Samples[0]))
		}
		if math.Abs(wav.Samples[0][0]-0.75) > 0.01 {
			t.Errorf("expected 0.75, got %f", wav.Samples[0][0])
		}
	})
}

// ============================================================================
// readWavFile error cases
// ============================================================================

func TestReadWavFileErrors(t *testing.T) {
	t.Run("file not found", func(t *testing.T) {
		_, err := readWavFile("/nonexistent/path/file.wav")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("unsupported audio format", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "unsupported.wav")

		buf := createTestWavBuffer(7, 16, 44100, 1, make([]byte, 4)) // Format 7 is mu-law
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for unsupported format")
		}
	})

	t.Run("block align zero", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.wav")

		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(36))
		buf.Write([]byte("WAVE"))
		buf.Write([]byte("fmt "))
		binary.Write(buf, binary.LittleEndian, uint32(16))
		binary.Write(buf, binary.LittleEndian, uint16(1))  // format
		binary.Write(buf, binary.LittleEndian, uint16(1))  // channels
		binary.Write(buf, binary.LittleEndian, uint32(44100))
		binary.Write(buf, binary.LittleEndian, uint32(0))  // byte rate
		binary.Write(buf, binary.LittleEndian, uint16(0))  // block align = 0
		binary.Write(buf, binary.LittleEndian, uint16(16))
		buf.Write([]byte("data"))
		binary.Write(buf, binary.LittleEndian, uint32(4))
		buf.Write([]byte{0, 0, 0, 0})

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for block align zero")
		}
	})

	t.Run("data size exceeds file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.wav")

		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(36))
		buf.Write([]byte("WAVE"))
		buf.Write([]byte("fmt "))
		binary.Write(buf, binary.LittleEndian, uint32(16))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint32(44100))
		binary.Write(buf, binary.LittleEndian, uint32(88200))
		binary.Write(buf, binary.LittleEndian, uint16(2))
		binary.Write(buf, binary.LittleEndian, uint16(16))
		buf.Write([]byte("data"))
		binary.Write(buf, binary.LittleEndian, uint32(1000000)) // Much larger than file
		buf.Write([]byte{0, 0})

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for data size exceeding file")
		}
	})

	t.Run("data size not aligned", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.wav")

		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(37))
		buf.Write([]byte("WAVE"))
		buf.Write([]byte("fmt "))
		binary.Write(buf, binary.LittleEndian, uint32(16))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint32(44100))
		binary.Write(buf, binary.LittleEndian, uint32(88200))
		binary.Write(buf, binary.LittleEndian, uint16(2)) // block align = 2
		binary.Write(buf, binary.LittleEndian, uint16(16))
		buf.Write([]byte("data"))
		binary.Write(buf, binary.LittleEndian, uint32(3)) // 3 bytes, not aligned to 2
		buf.Write([]byte{0, 0, 0})

		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for unaligned data size")
		}
	})

	t.Run("rejects oversized input", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.wav")

		buf := new(bytes.Buffer)
		buf.Write([]byte("RIFF"))
		binary.Write(buf, binary.LittleEndian, uint32(0))
		buf.Write([]byte("WAVE"))
		buf.Write([]byte("fmt "))
		binary.Write(buf, binary.LittleEndian, uint32(16))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint16(1))
		binary.Write(buf, binary.LittleEndian, uint32(44100))
		binary.Write(buf, binary.LittleEndian, uint32(44100*2))
		binary.Write(buf, binary.LittleEndian, uint16(2))
		binary.Write(buf, binary.LittleEndian, uint16(16))
		buf.Write([]byte("data"))
		binary.Write(buf, binary.LittleEndian, uint32(MaxInputDataSize+2)) // Too large

		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create failed: %v", err)
		}
		if _, err := f.Write(buf.Bytes()); err != nil {
			t.Fatalf("write failed: %v", err)
		}
		f.Close()

		_, err = readWavFile(path)
		if err == nil {
			t.Error("expected error for oversized input")
		}
	})

	t.Run("unsupported PCM bit depth", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.wav")

		buf := createTestWavBuffer(1, 12, 44100, 1, make([]byte, 6)) // 12-bit unsupported
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for unsupported bit depth")
		}
	})

	t.Run("unsupported float bit depth", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.wav")

		buf := createTestWavBuffer(3, 16, 44100, 1, make([]byte, 4)) // 16-bit float unsupported
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for unsupported float bit depth")
		}
	})

	t.Run("unsupported extensible subformat", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.wav")

		unknownFormat := [16]byte{0xFF, 0xFF, 0xFF, 0xFF}
		buf := createExtensibleWavBuffer(unknownFormat, 16, 16, 44100, 1, make([]byte, 4))
		if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		_, err := readWavFile(path)
		if err == nil {
			t.Error("expected error for unsupported extensible subformat")
		}
	})
}

// ============================================================================
// readWavInfo tests
// ============================================================================

func TestReadWavInfo(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.wav")

		samples := [][]float64{{0.1, 0.2, 0.3, 0.4}}
		if err := writeWavFile(path, samples, 44100, 1); err != nil {
			t.Fatalf("writeWavFile failed: %v", err)
		}

		info, err := readWavInfo(path)
		if err != nil {
			t.Fatalf("readWavInfo failed: %v", err)
		}
		if info.Path != path {
			t.Errorf("expected path %s, got %s", path, info.Path)
		}
		if info.SampleRate != 44100 {
			t.Errorf("expected sample rate 44100, got %d", info.SampleRate)
		}
		if info.Channels != 1 {
			t.Errorf("expected 1 channel, got %d", info.Channels)
		}
		if info.BitDepth != 16 {
			t.Errorf("expected 16-bit, got %d", info.BitDepth)
		}
		if info.NumSamples != 4 {
			t.Errorf("expected 4 samples, got %d", info.NumSamples)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := readWavInfo("/nonexistent/file.wav")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

// ============================================================================
// findWavFiles tests
// ============================================================================

func TestFindWavFiles(t *testing.T) {
	t.Run("finds matching files", func(t *testing.T) {
		dir := t.TempDir()

		// Create test WAV files
		samples := [][]float64{{0.1, 0.2}}
		writeWavFile(filepath.Join(dir, "kick_01.wav"), samples, 44100, 1)
		writeWavFile(filepath.Join(dir, "kick_02.wav"), samples, 44100, 1)
		writeWavFile(filepath.Join(dir, "snare_01.wav"), samples, 44100, 1)

		pattern := regexp.MustCompile(`(?i)^.*kick.*\.wav$`)
		files, err := findWavFiles(dir, pattern)
		if err != nil {
			t.Fatalf("findWavFiles failed: %v", err)
		}
		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}
	})

	t.Run("recursive search", func(t *testing.T) {
		dir := t.TempDir()
		subdir := filepath.Join(dir, "subdir")
		os.MkdirAll(subdir, 0755)

		samples := [][]float64{{0.1, 0.2}}
		writeWavFile(filepath.Join(dir, "kick_01.wav"), samples, 44100, 1)
		writeWavFile(filepath.Join(subdir, "kick_02.wav"), samples, 44100, 1)

		pattern := regexp.MustCompile(`(?i)^.*kick.*\.wav$`)
		files, err := findWavFiles(dir, pattern)
		if err != nil {
			t.Fatalf("findWavFiles failed: %v", err)
		}
		if len(files) != 2 {
			t.Errorf("expected 2 files from recursive search, got %d", len(files))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		dir := t.TempDir()

		samples := [][]float64{{0.1, 0.2}}
		writeWavFile(filepath.Join(dir, "snare_01.wav"), samples, 44100, 1)

		pattern := regexp.MustCompile(`(?i)^.*kick.*\.wav$`)
		files, err := findWavFiles(dir, pattern)
		if err != nil {
			t.Fatalf("findWavFiles failed: %v", err)
		}
		if len(files) != 0 {
			t.Errorf("expected 0 files, got %d", len(files))
		}
	})

	t.Run("sorted by filename", func(t *testing.T) {
		dir := t.TempDir()

		samples := [][]float64{{0.1, 0.2}}
		writeWavFile(filepath.Join(dir, "kick_03.wav"), samples, 44100, 1)
		writeWavFile(filepath.Join(dir, "kick_01.wav"), samples, 44100, 1)
		writeWavFile(filepath.Join(dir, "kick_02.wav"), samples, 44100, 1)

		pattern := regexp.MustCompile(`(?i)^.*kick.*\.wav$`)
		files, err := findWavFiles(dir, pattern)
		if err != nil {
			t.Fatalf("findWavFiles failed: %v", err)
		}
		if len(files) != 3 {
			t.Fatalf("expected 3 files, got %d", len(files))
		}
		if filepath.Base(files[0].Path) != "kick_01.wav" {
			t.Errorf("expected first file kick_01.wav, got %s", filepath.Base(files[0].Path))
		}
		if filepath.Base(files[1].Path) != "kick_02.wav" {
			t.Errorf("expected second file kick_02.wav, got %s", filepath.Base(files[1].Path))
		}
	})

	t.Run("invalid directory", func(t *testing.T) {
		pattern := regexp.MustCompile(`.*`)
		_, err := findWavFiles("/nonexistent/path", pattern)
		if err == nil {
			t.Error("expected error for nonexistent directory")
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		dir := t.TempDir()

		samples := [][]float64{{0.1, 0.2}}
		writeWavFile(filepath.Join(dir, "KICK_01.wav"), samples, 44100, 1)
		writeWavFile(filepath.Join(dir, "Kick_02.wav"), samples, 44100, 1)
		writeWavFile(filepath.Join(dir, "kick_03.wav"), samples, 44100, 1)

		pattern := regexp.MustCompile(`(?i)^.*kick.*\.wav$`)
		files, err := findWavFiles(dir, pattern)
		if err != nil {
			t.Fatalf("findWavFiles failed: %v", err)
		}
		if len(files) != 3 {
			t.Errorf("expected 3 files with case insensitive match, got %d", len(files))
		}
	})
}

// ============================================================================
// processBatch tests
// ============================================================================

func TestProcessBatch(t *testing.T) {
	t.Run("basic batch processing", func(t *testing.T) {
		dir := t.TempDir()
		tempDir := t.TempDir()
		outputDir := t.TempDir()

		// Create test WAV files
		samples := [][]float64{{0.5, 0.5, 0.5, 0.5}}
		path1 := filepath.Join(dir, "test1.wav")
		path2 := filepath.Join(dir, "test2.wav")
		writeWavFile(path1, samples, 44100, 1)
		writeWavFile(path2, samples, 44100, 1)

		files := []FileInfo{
			{Path: path1, SampleRate: 44100, Channels: 1, BitDepth: 16, NumSamples: 4},
			{Path: path2, SampleRate: 44100, Channels: 1, BitDepth: 16, NumSamples: 4},
		}

		outputFile := filepath.Join(outputDir, "output.wav")
		err := processBatch(files, 44100, 1, 100, tempDir, outputFile, false)
		if err != nil {
			t.Fatalf("processBatch failed: %v", err)
		}

		// Verify output file exists
		if _, err := os.Stat(outputFile); os.IsNotExist(err) {
			t.Error("output file was not created")
		}

		// Read and verify output
		wav, err := readWavFile(outputFile)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		// Should have 2 slices * 100 samples = 200 samples
		if len(wav.Samples[0]) != 200 {
			t.Errorf("expected 200 samples, got %d", len(wav.Samples[0]))
		}
	})

	t.Run("with normalization", func(t *testing.T) {
		dir := t.TempDir()
		tempDir := t.TempDir()
		outputDir := t.TempDir()

		samples := [][]float64{{0.25, 0.25, 0.25, 0.25}}
		path1 := filepath.Join(dir, "test1.wav")
		writeWavFile(path1, samples, 44100, 1)

		files := []FileInfo{
			{Path: path1, SampleRate: 44100, Channels: 1, BitDepth: 16, NumSamples: 4},
		}

		outputFile := filepath.Join(outputDir, "normalized.wav")
		err := processBatch(files, 44100, 1, 100, tempDir, outputFile, true)
		if err != nil {
			t.Fatalf("processBatch failed: %v", err)
		}

		wav, err := readWavFile(outputFile)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		// After normalization, peak should be near 1.0
		peak := 0.0
		for _, v := range wav.Samples[0] {
			if math.Abs(v) > peak {
				peak = math.Abs(v)
			}
		}
		if peak < 0.95 {
			t.Errorf("expected normalized peak near 1.0, got %f", peak)
		}
	})

	t.Run("resampling in batch", func(t *testing.T) {
		dir := t.TempDir()
		tempDir := t.TempDir()
		outputDir := t.TempDir()

		samples := [][]float64{{0.5, 0.5, 0.5, 0.5}}
		path1 := filepath.Join(dir, "test1.wav")
		writeWavFile(path1, samples, 48000, 1) // Different sample rate

		files := []FileInfo{
			{Path: path1, SampleRate: 48000, Channels: 1, BitDepth: 16, NumSamples: 4},
		}

		outputFile := filepath.Join(outputDir, "resampled.wav")
		err := processBatch(files, 44100, 1, 100, tempDir, outputFile, false) // Target 44100
		if err != nil {
			t.Fatalf("processBatch failed: %v", err)
		}

		wav, err := readWavFile(outputFile)
		if err != nil {
			t.Fatalf("readWavFile failed: %v", err)
		}
		if wav.Header.SampleRate != 44100 {
			t.Errorf("expected sample rate 44100, got %d", wav.Header.SampleRate)
		}
	})
}

// ============================================================================
// processFiles tests
// ============================================================================

func TestProcessFiles(t *testing.T) {
	t.Run("creates batches correctly", func(t *testing.T) {
		dir := t.TempDir()
		outputDir := t.TempDir()

		// Create 5 test files
		samples := [][]float64{{0.5, 0.5}}
		for i := 1; i <= 5; i++ {
			path := filepath.Join(dir, "test"+string(rune('0'+i))+".wav")
			writeWavFile(path, samples, 44100, 1)
		}

		// Find files
		pattern := regexp.MustCompile(`(?i)^.*test.*\.wav$`)
		files, _ := findWavFiles(dir, pattern)

		// Process with 2 slices per batch
		err := processFiles(files, 44100, 1, 2, 100, "test", outputDir, false)
		if err != nil {
			t.Fatalf("processFiles failed: %v", err)
		}

		// Should create 3 batches (2+2+1)
		batch1 := filepath.Join(outputDir, "test_2slices_batch001.wav")
		batch2 := filepath.Join(outputDir, "test_2slices_batch002.wav")
		batch3 := filepath.Join(outputDir, "test_2slices_batch003.wav")

		for _, f := range []string{batch1, batch2, batch3} {
			if _, err := os.Stat(f); os.IsNotExist(err) {
				t.Errorf("expected batch file %s not found", f)
			}
		}
	})
}

// ============================================================================
// writeBytes and writeLE tests
// ============================================================================

func TestWriteHelpers(t *testing.T) {
	t.Run("writeBytes", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := writeBytes(buf, []byte("RIFF"))
		if err != nil {
			t.Fatalf("writeBytes failed: %v", err)
		}
		if buf.String() != "RIFF" {
			t.Errorf("expected RIFF, got %s", buf.String())
		}
	})

	t.Run("writeLE uint32", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := writeLE(buf, uint32(44100))
		if err != nil {
			t.Fatalf("writeLE failed: %v", err)
		}
		if buf.Len() != 4 {
			t.Errorf("expected 4 bytes, got %d", buf.Len())
		}
		val := binary.LittleEndian.Uint32(buf.Bytes())
		if val != 44100 {
			t.Errorf("expected 44100, got %d", val)
		}
	})

	t.Run("writeLE uint16", func(t *testing.T) {
		buf := new(bytes.Buffer)
		err := writeLE(buf, uint16(1))
		if err != nil {
			t.Fatalf("writeLE failed: %v", err)
		}
		if buf.Len() != 2 {
			t.Errorf("expected 2 bytes, got %d", buf.Len())
		}
	})
}

// ============================================================================
// Edge case and integration tests
// ============================================================================

func TestSilenceRemovalIntegration(t *testing.T) {
	// Test that silence removal works correctly in the full pipeline
	dir := t.TempDir()

	// Create a WAV with leading silence
	samples := [][]float64{{0, 0, 0, 0.5, 1.0, 0.5}}
	path := filepath.Join(dir, "with_silence.wav")
	writeWavFile(path, samples, 44100, 1)

	wav, err := readWavFile(path)
	if err != nil {
		t.Fatalf("readWavFile failed: %v", err)
	}

	trimmed := removeLeadingSilence(wav.Samples)
	if len(trimmed[0]) != 3 {
		t.Errorf("expected 3 samples after trim, got %d", len(trimmed[0]))
	}
	if math.Abs(trimmed[0][0]-0.5) > 0.1 {
		t.Errorf("expected first sample ~0.5, got %f", trimmed[0][0])
	}
}

func TestChannelConversionIntegration(t *testing.T) {
	dir := t.TempDir()

	// Create stereo WAV
	samples := [][]float64{
		{0.5, 0.5, 0.5},
		{0.5, 0.5, 0.5},
	}
	path := filepath.Join(dir, "stereo.wav")
	writeWavFile(path, samples, 44100, 2)

	wav, err := readWavFile(path)
	if err != nil {
		t.Fatalf("readWavFile failed: %v", err)
	}

	// Convert to mono
	mono := convertChannels(wav.Samples, 1)
	if len(mono) != 1 {
		t.Errorf("expected 1 channel, got %d", len(mono))
	}
}

func TestResampleIntegration(t *testing.T) {
	dir := t.TempDir()

	// Create WAV at 48kHz
	samples := make([]float64, 4800) // 100ms at 48kHz
	for i := range samples {
		samples[i] = math.Sin(float64(i) * 2 * math.Pi * 440 / 48000) // 440Hz tone
	}
	path := filepath.Join(dir, "48k.wav")
	writeWavFile(path, [][]float64{samples}, 48000, 1)

	wav, err := readWavFile(path)
	if err != nil {
		t.Fatalf("readWavFile failed: %v", err)
	}

	// Resample to 44.1kHz
	resampled := resample(wav.Samples, 48000, 44100)

	// Expected length: 4800 * (44100/48000) ≈ 4410
	expectedLen := int(float64(4800) * 44100 / 48000)
	if math.Abs(float64(len(resampled[0])-expectedLen)) > 1 {
		t.Errorf("expected ~%d samples after resample, got %d", expectedLen, len(resampled[0]))
	}
}

// ============================================================================
// EOF handling test
// ============================================================================

func TestReadWavFileEarlyEOF(t *testing.T) {
	// Test that readWavFile handles truncated files gracefully
	dir := t.TempDir()
	path := filepath.Join(dir, "truncated.wav")

	// Create header claiming more data than actually present
	buf := new(bytes.Buffer)
	buf.Write([]byte("RIFF"))
	binary.Write(buf, binary.LittleEndian, uint32(44+100)) // Claims 100 bytes of data
	buf.Write([]byte("WAVE"))
	buf.Write([]byte("fmt "))
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1))
	binary.Write(buf, binary.LittleEndian, uint16(1))
	binary.Write(buf, binary.LittleEndian, uint32(44100))
	binary.Write(buf, binary.LittleEndian, uint32(88200))
	binary.Write(buf, binary.LittleEndian, uint16(2))
	binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.Write([]byte("data"))
	binary.Write(buf, binary.LittleEndian, uint32(8)) // Claims 8 bytes but only write 4
	buf.Write([]byte{0, 0, 0, 0})

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	wav, err := readWavFile(path)
	if err != nil {
		t.Fatalf("readWavFile should handle truncated files: %v", err)
	}
	// Should have read 2 samples (4 bytes / 2 bytes per sample)
	if len(wav.Samples[0]) != 2 {
		t.Errorf("expected 2 samples from truncated file, got %d", len(wav.Samples[0]))
	}
}

// ============================================================================
// Test error writer for writeWavFile
// ============================================================================

type errorWriter struct {
	failAfter int
	written   int
}

func (e *errorWriter) Write(p []byte) (n int, err error) {
	if e.written+len(p) > e.failAfter {
		return 0, io.ErrShortWrite
	}
	e.written += len(p)
	return len(p), nil
}

func TestWriteWavFileWithSparseChannels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse.wav")

	// Create samples where channel array is shorter than numChannels
	samples := [][]float64{{0.5, 0.5}}
	err := writeWavFile(path, samples, 44100, 2) // 2 channels but only 1 channel of data
	if err != nil {
		t.Fatalf("writeWavFile failed: %v", err)
	}

	wav, err := readWavFile(path)
	if err != nil {
		t.Fatalf("readWavFile failed: %v", err)
	}
	if wav.Header.NumChannels != 2 {
		t.Errorf("expected 2 channels, got %d", wav.Header.NumChannels)
	}
}
