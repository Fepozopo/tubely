package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// Set an upload limit
	const maxUpload = 1 << 30 // 1 GB
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)

	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid video ID", err)
		return
	}

	// Authenticate the user
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// Get the video's metadata from the SQLite database
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Couldn't get video", err)
		return
	}
	// Check if the user is the owner of the video
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You must be the video owner", nil)
		return
	}

	// Parse the form data
	const maxMemory = 1 << 30 // 1 GB
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing form data", err)
		return
	}

	// Get the file from the form data
	videoFile, _, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error getting file from form data", err)
		return
	}
	defer videoFile.Close()

	// Read the first 512 bytes to detect the content type
	fileHeader := make([]byte, 512)
	_, err = videoFile.Read(fileHeader)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reading file header", err)
		return
	}

	// Reset the read position to the start of the video file
	_, err = videoFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting video file read position", err)
		return
	}

	// Validate the uploaded file to ensure it's an MP4 video
	mediaType := http.DetectContentType(fileHeader)
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid video type", nil)
		return
	}

	// Create a temporary local file
	tmpLocalFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating temporary local file", err)
	}
	defer os.Remove(tmpLocalFile.Name()) // clean up
	defer tmpLocalFile.Close()

	// Copy the contents from the wire to the temp file
	_, err = io.Copy(tmpLocalFile, videoFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying file contents to temporary local file", err)
		return
	}

	// Get the aspect ratio of the video file
	aspectRatio, err := getVideoAspectRatio(tmpLocalFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting aspect ratio of video file", err)
	}
	var videoOrientation string
	switch aspectRatio {
	case "16:9":
		videoOrientation = "landscape"
	case "9:16":
		videoOrientation = "portrait"
	default:
		videoOrientation = "other"
	}

	// Reset the read position to the start of the temp file
	_, err = tmpLocalFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting temporary local file read position", err)
		return
	}

	// Create a processed version of the video for fast start
	fastStartVideoLocation, err := processVideoForFastStart(tmpLocalFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating a processed version of the video", err)
		return
	}

	// Open the processed video
	fastStartVideoFile, err := os.Open(fastStartVideoLocation)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening processed video file", err)
		return
	}
	defer os.Remove(fastStartVideoLocation) // clean up
	defer fastStartVideoFile.Close()

	// Fill a 32-byte slice with random bytes
	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error generating random bytes", err)
		return
	}
	// Convert random bytes to a hex string
	randomHex := hex.EncodeToString(randomBytes)

	// Put the object into S3 using PutObject
	fmt.Println("Uploading video to S3")
	_, err = cfg.s3Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(fmt.Sprintf("%s/%s.mp4", videoOrientation, randomHex)),
		Body:        fastStartVideoFile,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading to S3", err)
		return
	}

	// If the video already had a video URL, delete the old video in S3
	if video.VideoURL != nil {
		fmt.Println("Deleting old video from S3")
		oldVideoURL := *video.VideoURL

		// The url is in this format "bucket,key"
		// Extract everything after the "bucket"
		splitURL := strings.SplitN(oldVideoURL, ",", 2)
		if len(splitURL) < 2 {
			respondWithError(w, http.StatusInternalServerError, "Invalid video URL format", nil)
			return
		}
		oldVideoKey := splitURL[1]

		// Delete the old video
		_, err = cfg.s3Client.DeleteObject(context.TODO(), &s3.DeleteObjectInput{
			Bucket: aws.String(cfg.s3Bucket),
			Key:    aws.String(oldVideoKey),
		})
		if err != nil {
			respondWithError(w, http.StatusInternalServerError, "Error deleting old video in S3", err)
			return
		}
	}

	// Update the VideoURL of the video recorded in the database with the S3 bucket and key
	videoURL := fmt.Sprintf("%s,%s/%s.mp4", cfg.s3Bucket, videoOrientation, randomHex)
	video.VideoURL = &videoURL

	// Update the database with the new video URL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video in database", err)
		return
	}

	// Respond with updated JSON of the video's metadata
	fmt.Println("Done!")
	respondWithJSON(w, http.StatusOK, video)
}
