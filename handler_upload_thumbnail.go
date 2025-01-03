package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

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

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// Parse the form data
	const maxMemory = 10 << 20 // 10 MB
	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error parsing form data", err)
		return
	}

	// Get the file from the form data
	file, _, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Error getting file from form data", err)
		return
	}
	defer file.Close()

	// Get the Content-Type header from the file
	header := make([]byte, 512)
	_, err = file.Read(header)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reading file header", err)
		return
	}

	// Reset the read position to the start of the file
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error resetting file read position", err)
		return
	}

	mediaType := http.DetectContentType(header)

	// Use the Content-Type header to determine the file extension
	var fileExtension string
	switch mediaType {
	case "image/jpeg":
		fileExtension = ".jpg"
	case "image/png":
		fileExtension = ".png"
	default:
		respondWithError(w, http.StatusBadRequest, "Unsupported media type", nil)
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
		respondWithError(w, http.StatusUnauthorized, "You can't upload a thumbnail for this video", nil)
		return
	}

	// Fill a 32-byte slice with random bytes and convert it into a random base64 string
	randomBytes := make([]byte, 32)
	_, err = rand.Read(randomBytes)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error generating random bytes", err)
		return
	}
	randomString := base64.RawURLEncoding.EncodeToString(randomBytes)

	// Create the file name and file path
	fileName := fmt.Sprintf("%s%s", randomString, fileExtension)
	filePath := filepath.Join(cfg.assetsRoot, fileName)

	// Create the new file
	localFile, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating new file", err)
		return
	}
	defer localFile.Close()

	// Copy the image data to the new file
	_, err = io.Copy(localFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error copying image data to new file", err)
		return
	}

	// Delete the old thumbnail file if it exists
	if video.ThumbnailURL != nil {
		oldThumbnailPath := filepath.Join(cfg.assetsRoot, filepath.Base(*video.ThumbnailURL))
		err = os.Remove(oldThumbnailPath)
		if err != nil && !os.IsNotExist(err) {
			respondWithError(w, http.StatusInternalServerError, "Error deleting old thumbnail file", err)
			return
		}
	}

	// Update the video's new ThumbnailURL
	thumbnailURL := fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, fileName)
	video.ThumbnailURL = &thumbnailURL

	// Update the database with the new thumbnail URL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video in database", err)
		return
	}

	// Respond with updated JSON of the video's metadata
	respondWithJSON(w, http.StatusOK, video)

}
