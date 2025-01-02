package main

import (
	"fmt"
	"io"
	"net/http"

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
	err = r.ParseMultipartForm(10 << 20) // 10 MB
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
	// Get the media type from the file header
	mediaType := r.Header.Get("Content-Type")

	// Read the image data into a byte slice using io.ReadAll
	imageData, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error reading image data", err)
		return
	}

	// Get the video's metadata from the SQLite database using GetVideo
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

	// Save the thumbnail to the global map
	videoThumbnails[videoID] = thumbnail{
		data:      imageData,
		mediaType: mediaType,
	}

	// Update the database with the new thumbnail URL using UpdateVideo
	thumbnailURL := fmt.Sprintf("http://localhost:%s/api/thumbnails/%s", cfg.port, videoID)
	video.ThumbnailURL = &thumbnailURL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error updating video in database", err)
		return
	}

	// Respond with updated JSON of the video's metadata
	respondWithJSON(w, http.StatusOK, video)

}
