# =============================================
# Stage 1: Build
# =============================================
FROM golang:1.24-bookworm AS builder

# Install CGO dependencies (required for sqlite3 and go-fitz/MuPDF).
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libmupdf-dev \
    libmujs-dev \
    libgumbo-dev \
    libjpeg-dev \
    libopenjp2-7-dev \
    libfreetype6-dev \
    libharfbuzz-dev \
    pkg-config \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache Go module dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build.
COPY . .
RUN VERSION="$(tr -d ' \n\r\t' < VERSION)" && \
    CGO_ENABLED=1 GOOS=linux go build \
      -ldflags "-X megane/internal/version.LinkVersion=$$VERSION" \
      -o /app/megane ./cmd/server

# =============================================
# Stage 2: Runtime
# =============================================
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libmupdf-dev \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/megane ./megane
COPY static/  ./static/
COPY prompts/ ./prompts/

# Create writable directories.
RUN mkdir -p projects .db

EXPOSE 8080

ENV PORT=8080 \
    DB_PATH=/app/.db/megane.db \
    PROJECTS_DIR=/app/projects

ENTRYPOINT ["./megane"]
