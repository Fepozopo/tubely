package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// getVideoAspectRatio takes a file path and returns the aspect ratio as a string.
// It uses the ffprobe command line tool to retrieve the video's aspect ratio.
// The returned string is in the format "width:height".
func getVideoAspectRatio(filePath string) (string, error) {
	// Create a new command with the right arguments.
	// The -v flag specifies the log level.
	// The -print_format json flag specifies the output format.
	// The -show_streams flag prints information about the file.
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)

	// Run the command and capture the output.
	output, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("ffprobe failed: %s", string(output))
		}
		return "", fmt.Errorf("unexpected error running ffprobe: %v", err)
	}

	// Define a struct to unmarshal the JSON output into.
	type Stream struct {
		Width              int    `json:"width"`
		Height             int    `json:"height"`
		DisplayAspectRatio string `json:"display_aspect_ratio"`
	}
	type FFProbeOutput struct {
		Streams []Stream `json:"streams"`
	}

	// Unmarshal the output into the struct.
	var ffprobeOutput FFProbeOutput
	err = json.Unmarshal(output, &ffprobeOutput)
	if err != nil {
		return "", fmt.Errorf("error unmarshaling ffprobe output: %v", err)
	}

	// Find the first video stream and get the aspect ratio.
	for _, stream := range ffprobeOutput.Streams {
		return stream.DisplayAspectRatio, nil
	}

	return "", fmt.Errorf("couldn't find video stream in ffprobe output")
}

func processVideoForFastStart(filePath string) (string, error) {
	outputFilePath := filePath + ".processing"

	// Create a new command with the right arguments.
	// The -i filePath flag specifies the input file path.
	// The -c copy tells ffmpeg to copy the audio and video streams without re-encoding them.
	// The -movflags +faststart flag specifies to optimize for fast start.
	// The -f flag specifies the output format.
	// The output file path is specified as an argument.
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputFilePath)

	// Run the command and capture the output.
	output, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("ffmpeg failed: %s", string(output))
		}
		return "", fmt.Errorf("unexpected error running ffmpeg: %v", err)
	}

	return outputFilePath, nil
}
