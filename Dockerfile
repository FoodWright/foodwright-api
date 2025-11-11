# --- Build Stage ---
# Use a slim Go image for the builder stage
FROM golang:1.25 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum files to download dependencies first.
# This leverages Docker's layer caching, so dependencies are only re-downloaded
# if these files change.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code into the container
COPY . .

# Build the Go application as a static binary.
# CGO_ENABLED=0 is important for creating a static binary that doesn't depend on system C libraries.
# -o /api-server places the compiled binary named 'api-server' in the root directory.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -o /api-server ./cmd/api

# --- Final Stage ---
# Use a minimal 'distroless' or alpine base image for the final container for security. Alpine is simple and has a shell for debugging.
FROM alpine:latest

# We don't need any special capabilities, so run as a non-root user for security.
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

# Copy only the compiled binary from the builder stage into the final image.
COPY --from=builder /api-server /api-server

# Copy the database migrations so the application can run them on startup.
# This assumes your migrations are in a 'db/migrations' directory at the root.
COPY ./db/migrations ./db/migrations

# Expose the port the server will run on. The actual port is set by the PORT env var in Cloud Run.
EXPOSE 8080

# This command will be executed when the container starts.
ENTRYPOINT ["/api-server"]