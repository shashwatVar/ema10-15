# Use the official Golang image as the base image
FROM golang:1.22.2-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the source code into the container
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/ema10-15

# Use a smaller base image for the final stage
FROM alpine:latest

# Set the working directory
WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/main .

# Copy the .env file
COPY .env .

# Expose the port the app runs on
EXPOSE 8080

# Use ENTRYPOINT and CMD combination to allow arguments
ENTRYPOINT ["./main"]
CMD []