package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// WAV file constants
const (
	MaxTotalSamples  = 260000  // Maximum total sample frames (based on classic sampler limits)
	MaxInputDataSize = 1 << 30 // 1 GiB safety cap for input data
)

var (
	subFormatPCM   = [16]byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}
	subFormatFloat = [16]byte{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}
)

// WavHeader represents a WAV file header
type WavHeader struct {
	ChunkID        [4]byte // "RIFF"
	ChunkSize      uint32
	Format         [4]byte // "WAVE"
	Subchunk1ID    [4]byte // "fmt "
	Subchunk1Size  uint32
	AudioFormat    uint16 // 1 = PCM
	NumChannels    uint16
	SampleRate     uint32
	ByteRate       uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	ExtValidBits   uint16
	ExtChannelMask uint32
	ExtSubFormat   [16]byte
}

// WavFile represents a WAV file with its metadata and samples
type WavFile struct {
	Path       string
	Header     WavHeader
	Samples    [][]float64 // [channel][sample]
	DataSize   uint32
	FileSize   int64
	Duration   float64
	NumSamples int
}

// FileInfo stores information about found WAV files
type FileInfo struct {
	Path       string
	Size       int64
	SampleRate uint32
	Channels   uint16
	BitDepth   uint16
	Duration   float64
	NumSamples int
}

func main() {
	// Parse command line arguments
	workDir := flag.String("dir", ".", "Working directory to search for WAV files")
	pattern := flag.String("pattern", "", "File pattern to search for (e.g., 'kick')")
	sampleRate := flag.Int("rate", 44100, "Output sample rate in Hz (e.g., 44100, 22050, 14700, 11025)")
	stereo := flag.Bool("stereo", false, "Output stereo (default is mono)")
	sliceCount := flag.Int("slices", 32, "Number of slices per output file (1-64)")
	normalize := flag.Bool("normalize", false, "Normalize volume before saving combined output")
	outputDir := flag.String("output", ".", "Output directory for combined WAV files")
	flag.Parse()

	// Validate arguments
	if *pattern == "" {
		fmt.Println("Error: -pattern is required")
		flag.Usage()
		os.Exit(1)
	}

	if *sliceCount < 1 || *sliceCount > 64 {
		fmt.Println("Error: -slices must be between 1 and 64")
		os.Exit(1)
	}

	validRates := map[int]bool{44100: true, 22050: true, 14700: true, 11025: true}
	if !validRates[*sampleRate] {
		fmt.Println("Error: -rate must be one of: 44100, 22050, 14700, 11025")
		os.Exit(1)
	}

	// Calculate slice duration
	numChannels := 1
	if *stereo {
		numChannels = 2
	}

	maxSamples := MaxTotalSamples / numChannels
	samplesPerSlice := maxSamples / *sliceCount
	sliceDurationMs := float64(samplesPerSlice) / float64(*sampleRate) * 1000.0

	fmt.Println("=== WAV Sample Slicer ===")
	fmt.Printf("Working Directory: %s\n", *workDir)
	fmt.Printf("Pattern: %s\n", *pattern)
	fmt.Printf("Output Sample Rate: %d Hz\n", *sampleRate)
	fmt.Printf("Output Channels: %s\n", map[bool]string{true: "Stereo", false: "Mono"}[*stereo])
	fmt.Printf("Slice Count: %d\n", *sliceCount)
	fmt.Printf("Samples per Slice: %d\n", samplesPerSlice)
	fmt.Printf("Slice Duration: %.2f ms\n", sliceDurationMs)
	fmt.Printf("Max Total Duration: %.3f s\n", float64(maxSamples)/float64(*sampleRate))
	fmt.Println()

	// Build regex pattern from user input
	regexPattern := fmt.Sprintf("(?i)^.*%s.*\\.wav$", regexp.QuoteMeta(*pattern))
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		fmt.Printf("Error compiling regex: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Searching with regex: %s\n\n", regexPattern)

	// Find matching files
	files, err := findWavFiles(*workDir, re)
	if err != nil {
		fmt.Printf("Error searching for files: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Println("No matching WAV files found.")
		os.Exit(0)
	}

	// Display summary
	displaySummary(files)

	// Ask for confirmation
	fmt.Print("\nProceed with processing? (y/n): ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Aborted.")
		os.Exit(0)
	}

	// Create output directory if it doesn't exist
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Process files in batches
	err = processFiles(files, *sampleRate, numChannels, *sliceCount, samplesPerSlice, *pattern, *outputDir, *normalize)
	if err != nil {
		fmt.Printf("Error processing files: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nProcessing complete!")
}

// findWavFiles recursively searches for WAV files matching the pattern
func findWavFiles(root string, pattern *regexp.Regexp) ([]FileInfo, error) {
	var files []FileInfo

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if pattern.MatchString(info.Name()) {
			// Read WAV header to get metadata
			wavInfo, err := readWavInfo(path)
			if err != nil {
				fmt.Printf("Warning: Could not read %s: %v\n", path, err)
				return nil
			}
			wavInfo.Size = info.Size()
			files = append(files, wavInfo)
		}

		return nil
	})

	// Sort by filename
	sort.Slice(files, func(i, j int) bool {
		return filepath.Base(files[i].Path) < filepath.Base(files[j].Path)
	})

	return files, err
}

// readWavInfo reads the WAV file header to extract metadata
func readWavInfo(path string) (FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileInfo{}, err
	}
	defer f.Close()

	header, dataSize, err := readWavHeader(f)
	if err != nil {
		return FileInfo{}, err
	}

	numSamples := int(dataSize) / int(header.NumChannels) / int(header.BitsPerSample/8)
	duration := float64(numSamples) / float64(header.SampleRate)

	return FileInfo{
		Path:       path,
		SampleRate: header.SampleRate,
		Channels:   header.NumChannels,
		BitDepth:   header.BitsPerSample,
		Duration:   duration,
		NumSamples: numSamples,
	}, nil
}

// readWavHeader reads and parses a WAV file header
func readWavHeader(r io.ReadSeeker) (WavHeader, uint32, error) {
	var header WavHeader
	var dataSize uint32

	// Read RIFF header
	if err := binary.Read(r, binary.LittleEndian, &header.ChunkID); err != nil {
		return header, 0, err
	}
	if string(header.ChunkID[:]) != "RIFF" {
		return header, 0, fmt.Errorf("not a valid WAV file (missing RIFF)")
	}

	if err := binary.Read(r, binary.LittleEndian, &header.ChunkSize); err != nil {
		return header, 0, err
	}

	if err := binary.Read(r, binary.LittleEndian, &header.Format); err != nil {
		return header, 0, err
	}
	if string(header.Format[:]) != "WAVE" {
		return header, 0, fmt.Errorf("not a valid WAV file (missing WAVE)")
	}

	// Read chunks until we find fmt and data
	fmtFound := false
	dataFound := false

	for !dataFound {
		var chunkID [4]byte
		var chunkSize uint32

		if err := binary.Read(r, binary.LittleEndian, &chunkID); err != nil {
			if err == io.EOF {
				break
			}
			return header, 0, err
		}
		if err := binary.Read(r, binary.LittleEndian, &chunkSize); err != nil {
			return header, 0, err
		}

		switch string(chunkID[:]) {
		case "fmt ":
			header.Subchunk1ID = chunkID
			header.Subchunk1Size = chunkSize

			if chunkSize < 16 {
				return header, 0, fmt.Errorf("invalid fmt chunk size: %d", chunkSize)
			}

			if err := binary.Read(r, binary.LittleEndian, &header.AudioFormat); err != nil {
				return header, 0, err
			}
			if err := binary.Read(r, binary.LittleEndian, &header.NumChannels); err != nil {
				return header, 0, err
			}
			if err := binary.Read(r, binary.LittleEndian, &header.SampleRate); err != nil {
				return header, 0, err
			}
			if err := binary.Read(r, binary.LittleEndian, &header.ByteRate); err != nil {
				return header, 0, err
			}
			if err := binary.Read(r, binary.LittleEndian, &header.BlockAlign); err != nil {
				return header, 0, err
			}
			if err := binary.Read(r, binary.LittleEndian, &header.BitsPerSample); err != nil {
				return header, 0, err
			}

			// Read any extra bytes in fmt chunk (for extensible format)
			if chunkSize > 16 {
				extraSize := int(chunkSize - 16)
				extra := make([]byte, extraSize)
				if _, err := io.ReadFull(r, extra); err != nil {
					return header, 0, err
				}
				if header.AudioFormat == 0xFFFE {
					// Extensible format extension layout (after basic 16-byte fmt):
					// extra[0:2]  = cbSize (extension size, typically 22)
					// extra[2:4]  = wValidBitsPerSample
					// extra[4:8]  = dwChannelMask
					// extra[8:24] = SubFormat GUID
					if len(extra) < 24 {
						return header, 0, fmt.Errorf("invalid extensible fmt chunk size")
					}
					header.ExtValidBits = binary.LittleEndian.Uint16(extra[2:4])
					header.ExtChannelMask = binary.LittleEndian.Uint32(extra[4:8])
					copy(header.ExtSubFormat[:], extra[8:24])
				}
			}
			fmtFound = true

		case "data":
			if !fmtFound {
				return header, 0, fmt.Errorf("data chunk found before fmt chunk")
			}
			dataSize = chunkSize
			dataFound = true

		default:
			// Skip unknown chunks
			if _, err := r.Seek(int64(chunkSize), io.SeekCurrent); err != nil {
				return header, 0, err
			}
		}
	}

	if !fmtFound {
		return header, 0, fmt.Errorf("fmt chunk not found")
	}
	if !dataFound {
		return header, 0, fmt.Errorf("data chunk not found")
	}

	return header, dataSize, nil
}

// displaySummary shows a summary of found files
func displaySummary(files []FileInfo) {
	fmt.Printf("Found %d matching WAV files:\n", len(files))
	fmt.Println(strings.Repeat("-", 100))
	fmt.Printf("%-50s %10s %8s %8s %10s %12s\n", "File", "Size", "Rate", "Ch", "Bits", "Duration")
	fmt.Println(strings.Repeat("-", 100))

	var totalSize int64
	var totalDuration float64
	rateMap := make(map[uint32]int)
	channelMap := make(map[uint16]int)

	for _, f := range files {
		name := filepath.Base(f.Path)
		if len(name) > 48 {
			name = name[:45] + "..."
		}
		fmt.Printf("%-50s %10s %6dHz %8d %8d %10.3fs\n",
			name,
			formatSize(f.Size),
			f.SampleRate,
			f.Channels,
			f.BitDepth,
			f.Duration)

		totalSize += f.Size
		totalDuration += f.Duration
		rateMap[f.SampleRate]++
		channelMap[f.Channels]++
	}

	fmt.Println(strings.Repeat("-", 100))
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Total files: %d\n", len(files))
	fmt.Printf("  Total size: %s\n", formatSize(totalSize))
	fmt.Printf("  Total duration: %.2fs\n", totalDuration)
	fmt.Printf("  Sample rates: ")
	for rate, count := range rateMap {
		fmt.Printf("%dHz (%d files) ", rate, count)
	}
	fmt.Println()
	fmt.Printf("  Channels: ")
	for ch, count := range channelMap {
		chStr := "mono"
		if ch == 2 {
			chStr = "stereo"
		} else if ch > 2 {
			chStr = fmt.Sprintf("%d-ch", ch)
		}
		fmt.Printf("%s (%d files) ", chStr, count)
	}
	fmt.Println()
}

// formatSize formats a byte size to human readable format
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// processFiles processes all files in batches
func processFiles(files []FileInfo, targetRate, numChannels, sliceCount, samplesPerSlice int, pattern, outputDir string, normalize bool) error {
	// Create temp directory
	tempDir, err := os.MkdirTemp("", "wavslice-")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	fmt.Printf("\nUsing temp directory: %s\n", tempDir)

	batchNum := 0
	for i := 0; i < len(files); i += sliceCount {
		batchNum++
		end := i + sliceCount
		if end > len(files) {
			end = len(files)
		}

		batchFiles := files[i:end]
		fmt.Printf("\n=== Processing Batch %d (%d files) ===\n", batchNum, len(batchFiles))

		// Process batch
		outputFile := filepath.Join(outputDir, fmt.Sprintf("%s_%dslices_batch%03d.wav", sanitizeFilename(pattern), sliceCount, batchNum))
		err := processBatch(batchFiles, targetRate, numChannels, samplesPerSlice, tempDir, outputFile, normalize)
		if err != nil {
			return fmt.Errorf("failed to process batch %d: %v", batchNum, err)
		}

		fmt.Printf("Created: %s\n", outputFile)
	}

	return nil
}

// processBatch processes a single batch of files
func processBatch(files []FileInfo, targetRate, numChannels, samplesPerSlice int, tempDir, outputFile string, normalize bool) error {
	var processedSamples [][][]float64 // [file][channel][sample]

	for idx, f := range files {
		fmt.Printf("  Processing %d/%d: %s\n", idx+1, len(files), filepath.Base(f.Path))

		// Read the WAV file
		wav, err := readWavFile(f.Path)
		if err != nil {
			return fmt.Errorf("failed to read %s: %v", f.Path, err)
		}

		// Convert to target format
		samples := wav.Samples

		// Resample if needed
		if int(wav.Header.SampleRate) != targetRate {
			samples = resample(samples, int(wav.Header.SampleRate), targetRate)
		}

		// Convert channels if needed
		samples = convertChannels(samples, numChannels)

		// Remove leading silence
		samples = removeLeadingSilence(samples)

		// Truncate or pad to match slice duration
		samples = padOrTruncate(samples, samplesPerSlice)

		// Save normalized slice to temp directory
		tempPath := filepath.Join(tempDir, fmt.Sprintf("slice_%03d.wav", idx+1))
		if err := writeWavFile(tempPath, samples, targetRate, numChannels); err != nil {
			return fmt.Errorf("failed to write temp slice %s: %v", tempPath, err)
		}

		processedSamples = append(processedSamples, samples)
	}

	// Concatenate all processed samples
	concatenated := concatenateSamples(processedSamples, numChannels)

	if normalize {
		concatenated = normalizeSamples(concatenated)
	}

	// Write output file
	return writeWavFile(outputFile, concatenated, targetRate, numChannels)
}

// readWavFile reads a complete WAV file including samples
func readWavFile(path string) (*WavFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fileSize := stat.Size()

	header, dataSize, err := readWavHeader(f)
	if err != nil {
		return nil, err
	}

	if header.BlockAlign == 0 {
		return nil, fmt.Errorf("invalid WAV header: block align is zero")
	}
	if dataSize == 0 {
		return nil, fmt.Errorf("invalid WAV header: data size is zero")
	}
	if dataSize > MaxInputDataSize {
		return nil, fmt.Errorf("input data too large: %d bytes", dataSize)
	}
	if int64(dataSize) > fileSize {
		return nil, fmt.Errorf("invalid WAV header: data size exceeds file size")
	}
	if dataSize%uint32(header.BlockAlign) != 0 {
		return nil, fmt.Errorf("invalid WAV header: data size not aligned to block size")
	}

	// Determine the actual audio format
	// 1 = PCM, 3 = IEEE float, 0xFFFE = Extensible (treat as PCM or float based on bits)
	isFloat := header.AudioFormat == 3
	isPCM := header.AudioFormat == 1
	isExtensible := header.AudioFormat == 0xFFFE

	if !isPCM && !isFloat && !isExtensible {
		return nil, fmt.Errorf("unsupported audio format: %d (supported: 1=PCM, 3=IEEE Float, 65534=Extensible)", header.AudioFormat)
	}

	// For extensible format, determine if it's float or PCM based on subformat GUID
	if isExtensible {
		switch header.ExtSubFormat {
		case subFormatPCM:
			isPCM = true
		case subFormatFloat:
			isFloat = true
		default:
			return nil, fmt.Errorf("unsupported extensible subformat")
		}
	}

	// Read sample data
	numSamples := int(dataSize) / int(header.NumChannels) / int(header.BitsPerSample/8)
	samples := make([][]float64, header.NumChannels)
	for i := range samples {
		samples[i] = make([]float64, numSamples)
	}

	bytesPerSample := int(header.BitsPerSample) / 8
	buffer := make([]byte, bytesPerSample)

	for i := 0; i < numSamples; i++ {
		for ch := 0; ch < int(header.NumChannels); ch++ {
			_, err := io.ReadFull(f, buffer)
			if err != nil {
				if err == io.EOF {
					// Truncate to actual samples read
					for c := range samples {
						samples[c] = samples[c][:i]
					}
					goto done
				}
				return nil, err
			}

			var sample float64

			if isFloat {
				// IEEE Float format
				switch header.BitsPerSample {
				case 32:
					bits := binary.LittleEndian.Uint32(buffer)
					sample = float64(math.Float32frombits(bits))
				case 64:
					bits := binary.LittleEndian.Uint64(buffer)
					sample = math.Float64frombits(bits)
				default:
					return nil, fmt.Errorf("unsupported float bit depth: %d", header.BitsPerSample)
				}
			} else {
				// PCM format
				switch header.BitsPerSample {
				case 8:
					// 8-bit is unsigned
					sample = (float64(buffer[0]) - 128) / 128.0
				case 16:
					// 16-bit is signed
					val := int16(binary.LittleEndian.Uint16(buffer))
					sample = float64(val) / 32768.0
				case 24:
					// 24-bit is signed
					val := int32(buffer[0]) | int32(buffer[1])<<8 | int32(buffer[2])<<16
					if val&0x800000 != 0 {
						val |= ^0xFFFFFF // Sign extend
					}
					sample = float64(val) / 8388608.0
				case 32:
					// 32-bit is signed integer
					val := int32(binary.LittleEndian.Uint32(buffer))
					sample = float64(val) / 2147483648.0
				default:
					return nil, fmt.Errorf("unsupported PCM bit depth: %d", header.BitsPerSample)
				}
			}

			samples[ch][i] = sample
		}
	}

done:
	numSamplesActual := len(samples[0])
	duration := float64(numSamplesActual) / float64(header.SampleRate)

	return &WavFile{
		Path:       path,
		Header:     header,
		Samples:    samples,
		DataSize:   dataSize,
		FileSize:   fileSize,
		Duration:   duration,
		NumSamples: numSamplesActual,
	}, nil
}

// resample resamples audio using linear interpolation
func resample(samples [][]float64, fromRate, toRate int) [][]float64 {
	if fromRate == toRate {
		return samples
	}

	ratio := float64(fromRate) / float64(toRate)
	newLen := int(float64(len(samples[0])) / ratio)

	result := make([][]float64, len(samples))
	for ch := range samples {
		result[ch] = make([]float64, newLen)
		for i := 0; i < newLen; i++ {
			srcIdx := float64(i) * ratio
			srcIdxInt := int(srcIdx)
			frac := srcIdx - float64(srcIdxInt)

			if srcIdxInt+1 < len(samples[ch]) {
				// Linear interpolation
				result[ch][i] = samples[ch][srcIdxInt]*(1-frac) + samples[ch][srcIdxInt+1]*frac
			} else if srcIdxInt < len(samples[ch]) {
				result[ch][i] = samples[ch][srcIdxInt]
			}
		}
	}

	return result
}

// convertChannels converts between mono and stereo
func convertChannels(samples [][]float64, targetChannels int) [][]float64 {
	currentChannels := len(samples)

	if currentChannels == targetChannels {
		return samples
	}

	result := make([][]float64, targetChannels)
	numSamples := len(samples[0])

	if targetChannels == 1 && currentChannels >= 2 {
		// Convert to mono by averaging channels
		result[0] = make([]float64, numSamples)
		for i := 0; i < numSamples; i++ {
			sum := 0.0
			for ch := 0; ch < currentChannels; ch++ {
				sum += samples[ch][i]
			}
			result[0][i] = sum / float64(currentChannels)
		}
	} else if targetChannels == 2 && currentChannels == 1 {
		// Convert mono to stereo by duplicating
		result[0] = make([]float64, numSamples)
		result[1] = make([]float64, numSamples)
		copy(result[0], samples[0])
		copy(result[1], samples[0])
	} else {
		// For other cases, just take what we need or pad with zeros
		for ch := 0; ch < targetChannels; ch++ {
			result[ch] = make([]float64, numSamples)
			if ch < currentChannels {
				copy(result[ch], samples[ch])
			}
		}
	}

	return result
}

// removeLeadingSilence removes leading zero/near-zero samples
func removeLeadingSilence(samples [][]float64) [][]float64 {
	if len(samples) == 0 || len(samples[0]) == 0 {
		return samples
	}

	threshold := 0.001 // About -60dB
	startIdx := 0

	for i := 0; i < len(samples[0]); i++ {
		isSilent := true
		for ch := 0; ch < len(samples); ch++ {
			if math.Abs(samples[ch][i]) > threshold {
				isSilent = false
				break
			}
		}
		if !isSilent {
			startIdx = i
			break
		}
		startIdx = i + 1
	}

	if startIdx >= len(samples[0]) {
		// All silent, return a tiny bit of silence
		result := make([][]float64, len(samples))
		for ch := range result {
			result[ch] = make([]float64, 1)
		}
		return result
	}

	if startIdx == 0 {
		return samples
	}

	result := make([][]float64, len(samples))
	for ch := range samples {
		result[ch] = samples[ch][startIdx:]
	}

	return result
}

// padOrTruncate ensures samples are exactly the target length
func padOrTruncate(samples [][]float64, targetLength int) [][]float64 {
	if len(samples) == 0 {
		return samples
	}

	currentLength := len(samples[0])
	result := make([][]float64, len(samples))

	for ch := range samples {
		result[ch] = make([]float64, targetLength)
		if currentLength >= targetLength {
			// Truncate
			copy(result[ch], samples[ch][:targetLength])
		} else {
			// Pad with zeros
			copy(result[ch], samples[ch])
			// Rest is already zeros
		}
	}

	return result
}

// concatenateSamples concatenates multiple sample arrays into one
func concatenateSamples(allSamples [][][]float64, numChannels int) [][]float64 {
	if len(allSamples) == 0 {
		return make([][]float64, numChannels)
	}

	totalLength := 0
	for _, s := range allSamples {
		if len(s) > 0 {
			totalLength += len(s[0])
		}
	}

	result := make([][]float64, numChannels)
	for ch := range result {
		result[ch] = make([]float64, totalLength)
	}

	offset := 0
	for _, s := range allSamples {
		if len(s) == 0 {
			continue
		}
		sampleLen := len(s[0])
		for ch := 0; ch < numChannels; ch++ {
			if ch < len(s) {
				copy(result[ch][offset:], s[ch])
			}
		}
		offset += sampleLen
	}

	return result
}

// normalizeSamples scales audio so peak amplitude reaches 1.0
func normalizeSamples(samples [][]float64) [][]float64 {
	if len(samples) == 0 || len(samples[0]) == 0 {
		return samples
	}

	peak := 0.0
	for ch := range samples {
		for i := range samples[ch] {
			v := math.Abs(samples[ch][i])
			if v > peak {
				peak = v
			}
		}
	}

	if peak == 0 {
		return samples
	}

	scale := 1.0 / peak
	for ch := range samples {
		for i := range samples[ch] {
			samples[ch][i] *= scale
		}
	}

	return samples
}

func writeBytes(w io.Writer, b []byte) error {
	_, err := w.Write(b)
	return err
}

func writeLE(w io.Writer, data interface{}) error {
	return binary.Write(w, binary.LittleEndian, data)
}

// writeWavFile writes samples to a WAV file
func writeWavFile(path string, samples [][]float64, sampleRate, numChannels int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	numSamples := 0
	if len(samples) > 0 {
		numSamples = len(samples[0])
	}

	bitsPerSample := uint16(16)
	bytesPerSample := bitsPerSample / 8
	blockAlign := uint16(numChannels) * bytesPerSample
	byteRate := uint32(sampleRate) * uint32(blockAlign)
	dataSize := uint32(numSamples) * uint32(numChannels) * uint32(bytesPerSample)

	// Write RIFF header
	if err := writeBytes(f, []byte("RIFF")); err != nil {
		return err
	}
	if err := writeLE(f, uint32(36+dataSize)); err != nil {
		return err
	}
	if err := writeBytes(f, []byte("WAVE")); err != nil {
		return err
	}

	// Write fmt chunk
	if err := writeBytes(f, []byte("fmt ")); err != nil {
		return err
	}
	if err := writeLE(f, uint32(16)); err != nil { // Subchunk1Size
		return err
	}
	if err := writeLE(f, uint16(1)); err != nil { // AudioFormat (PCM)
		return err
	}
	if err := writeLE(f, uint16(numChannels)); err != nil {
		return err
	}
	if err := writeLE(f, uint32(sampleRate)); err != nil {
		return err
	}
	if err := writeLE(f, byteRate); err != nil {
		return err
	}
	if err := writeLE(f, blockAlign); err != nil {
		return err
	}
	if err := writeLE(f, bitsPerSample); err != nil {
		return err
	}

	// Write data chunk
	if err := writeBytes(f, []byte("data")); err != nil {
		return err
	}
	if err := writeLE(f, dataSize); err != nil {
		return err
	}

	// Write samples (interleaved)
	for i := 0; i < numSamples; i++ {
		for ch := 0; ch < numChannels; ch++ {
			var sample float64
			if ch < len(samples) && i < len(samples[ch]) {
				sample = samples[ch][i]
			}

			// Clamp to [-1, 1]
			if sample > 1.0 {
				sample = 1.0
			} else if sample < -1.0 {
				sample = -1.0
			}

			// Convert to 16-bit
			val := int16(sample * 32767)
			if err := writeLE(f, val); err != nil {
				return err
			}
		}
	}

	return nil
}

// sanitizeFilename removes invalid characters from filename
func sanitizeFilename(s string) string {
	// Replace invalid characters with underscore
	invalid := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	return invalid.ReplaceAllString(s, "_")
}
