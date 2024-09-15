package main

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var (
	imageDir     string
	assetDir     string
	logFile      string
	port         string
	timezoneName string
	logger       *log.Logger
	location     *time.Location
	imageMutex   = make(chan struct{}, 1) // Mutex to prevent concurrent writes
)

func init() {
	// Placeholder for timezone initialization
}

func main() {
	// Command-line flags
	flag.StringVar(&imageDir, "imagedir", getEnv("IMAGE_DIR", "images"), "Directory containing all images")
	flag.StringVar(&assetDir, "assetdir", getEnv("ASSET_DIR", "assets"), "Directory for assets (serving the image)")
	flag.StringVar(&logFile, "logfile", getEnv("LOG_FILE", ""), "Log file path (leave empty to disable file logging)")
	flag.StringVar(&port, "port", getEnv("PORT", "8080"), "Port to serve (default 8080)")
	flag.StringVar(&timezoneName, "timezone", getEnv("TIMEZONE", "CET"), "Timezone for image renewal (default CET)")
	flag.Parse()

	// Load the specified timezone
	var err error
	location, err = time.LoadLocation(timezoneName)
	if err != nil {
		log.Fatalf("Failed to load timezone '%s': %v", timezoneName, err)
	}

	// Set up logging
	logger = log.New(os.Stdout, "", log.LstdFlags)
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			logger.Printf("Failed to open log file: %v", err)
		} else {
			multiWriter := io.MultiWriter(os.Stdout, file)
			logger = log.New(multiWriter, "", log.LstdFlags)
			defer file.Close()
		}
	}

	// Ensure asset directory exists
	err = os.MkdirAll(assetDir, 0755)
	if err != nil {
		logger.Fatalf("Failed to create asset directory: %v", err)
	}

	// Initial image update
	updateImageForToday()

	// Schedule image updates
	go scheduleImageUpdates()

	// Serve HTTP
	http.HandleFunc("/", servePage)
	http.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetDir))))

	// Serve todays image for favicon
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(assetDir, assetImageFilename))
	})

	logger.Printf("Server started on :%s. Images will be renewed at midnight in timezone '%s'.", port, timezoneName)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Fatalf("Server failed: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func scheduleImageUpdates() {
	for {
		now := time.Now().In(location)
		// Compute next midnight in the specified timezone
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
		duration := nextMidnight.Sub(now)

		logger.Printf("Next image update in %v", duration)

		time.Sleep(duration)
		updateImageForToday()
	}
}

var assetImageFilename = ""

func updateImageForToday() {
	imageMutex <- struct{}{}        // Lock
	defer func() { <-imageMutex }() // Unlock

	logger.Println("Updating image for today...")

	// Get list of images
	images, err := getImageList(imageDir)
	if err != nil {
		logger.Printf("Error getting image list: %v", err)
		return
	}

	if len(images) == 0 {
		logger.Println("No images available in the image directory")
		return
	}

	// Create ImageMapper
	mapper := NewImageMapper(images)

	// Get image for today
	today := time.Now().In(location)
	selectedImage, err := mapper.GetImageForDate(today)
	if err != nil {
		logger.Printf("Error selecting image for today: %v", err)
		return
	}

	// Remove existing image in asset directory
	if assetImageFilename != "" {
		err = os.Remove(filepath.Join(assetDir, assetImageFilename))
		if err == nil {
			logger.Printf("Removed previous image: %s", assetImageFilename)
		} else {
			logger.Printf("Error removing previous image: %v", err)
		}
	} else {
		logger.Println("No previous image to remove :) ")
	}

	// Copy selected image to asset directory with a unique name
	srcPath := filepath.Join(imageDir, selectedImage)

	newImageName := fmt.Sprintf("today_%s.jpg", today.Format("2006-01-02"))
	destPath := filepath.Join(assetDir, newImageName)

	err = copyFile(srcPath, destPath)
	if err != nil {
		logger.Printf("Error copying image to asset directory: %v", err)
		return
	}
	assetImageFilename = newImageName

	logger.Printf("Today's image: %s", selectedImage)
}

func getImageList(dir string) ([]string, error) {
	var images []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Check if it's a file and has .jpg or .jpeg extension
		if !info.IsDir() && (filepath.Ext(info.Name()) == ".jpg" || filepath.Ext(info.Name()) == ".jpeg") {
			images = append(images, info.Name())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return images, nil
}

func copyFile(src, dst string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()

	// Overwrite the file if it exists
	output, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, input)
	return err
}

func servePage(w http.ResponseWriter, r *http.Request) {
	logger.Printf("request from %s: %s %s", r.RemoteAddr, r.Method, r.URL.Path)
	htmlContent := `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Image of the Day</title>
    <style>
        body {
            background-color: #121212;
            color: #ffffff;
            font-family: Arial, sans-serif;
            text-align: center;
            margin: 0;
            padding: 0;
            overflow: hidden;
        }
        h1 {
            margin-top: 20px;
        }
        p {
            margin-bottom: 20px;
        }
        img {
            max-width: 100%;
            max-height: calc(100vh - 140px);
            border-radius: 15px;
        }
    </style>
</head>
<body>
    <h1>Image of the Day</h1>
    <p>Enjoy a new image every day!</p>
	<img src="/assets/` + assetImageFilename + `" alt="Image of the Day">
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlContent)
}

//
// ImageMapper implementation
//

type ImageMapper struct {
	images []string
}

// NewImageMapper creates a new ImageMapper with a list of image names.
// The images should be sorted to ensure consistent ordering.
func NewImageMapper(images []string) *ImageMapper {
	// Make a copy of the images slice to prevent external modifications.
	imgs := make([]string, len(images))
	copy(imgs, images)
	// Sort the images to ensure consistent ordering.
	sort.Strings(imgs)
	return &ImageMapper{images: imgs}
}

// GetImageForDate returns the image name for a given date.
func (im *ImageMapper) GetImageForDate(date time.Time) (string, error) {
	if len(im.images) == 0 {
		return "", errors.New("image list is empty")
	}

	// Ensure the date is not in the future.
	today := time.Now().In(location)
	if date.After(today) {
		return "", errors.New("date is in the future")
	}

	// Ensure the date is not before the epoch (Jan 1, 2000).
	epoch := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if date.Before(epoch) {
		return "", errors.New("date is before the supported range (Jan 1, 2000)")
	}

	// Convert the date to a string in a consistent format.
	dateStr := date.Format("2006-01-02")
	dateHash := sha256.Sum256([]byte(dateStr))

	var maxScore uint64
	var selectedImage string

	for _, img := range im.images {
		// Combine the date hash with the image name.
		combined := append(dateHash[:], []byte(img)...)
		hash := sha256.Sum256(combined)

		// Convert the first 8 bytes of the hash to a uint64 for scoring.
		score := binary.BigEndian.Uint64(hash[:8])

		// Select the image with the highest score.
		if score > maxScore || selectedImage == "" {
			maxScore = score
			selectedImage = img
		}
	}

	return selectedImage, nil
}
