# Use the official Golang image
FROM golang:1.23

# Set the working directory
WORKDIR /app

# Copy the source code
COPY main.go go.mod .

# Build the Go application
RUN go build -o motd

# Expose port 8080
EXPOSE 8001

# Set environment variables if needed
ENV IMAGE_DIR=/app/images
ENV ASSET_DIR=/app/live
ENV PORT=8001

# Create the necessary directories
RUN mkdir -p $IMAGE_DIR $ASSET_DIR

# Copy images to the image directory
COPY images/* $IMAGE_DIR/

# Command to run the executable
CMD ["./monkey-app"]

