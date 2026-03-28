FROM golang:1.26-bookworm

# Fyne build dependencies (Linux native: GL + X11)
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libgl1-mesa-dev \
    libx11-dev \
    libxcursor-dev \
    libxrandr-dev \
    libxinerama-dev \
    libxi-dev \
    libxext-dev \
    libxxf86vm-dev \
    pkg-config \
    # Windows amd64 cross-compiler
    gcc-mingw-w64-x86-64 \
    # Utilities
    zip \
    && rm -rf /var/lib/apt/lists/*

# Install llvm-mingw for Windows arm64 cross-compilation
ARG TARGETARCH
RUN if [ "$TARGETARCH" = "amd64" ]; then \
        LLVM_ARCH="x86_64"; \
    else \
        LLVM_ARCH="aarch64"; \
    fi && \
    apt-get update && apt-get install -y --no-install-recommends curl xz-utils && \
    curl -fSL "https://github.com/mstorsjo/llvm-mingw/releases/download/20260324/llvm-mingw-20260324-ucrt-ubuntu-22.04-${LLVM_ARCH}.tar.xz" \
        -o /tmp/llvm-mingw.tar.xz && \
    tar -xf /tmp/llvm-mingw.tar.xz -C /opt && \
    mv /opt/llvm-mingw-* /opt/llvm-mingw && \
    rm /tmp/llvm-mingw.tar.xz && \
    apt-get purge -y curl xz-utils && apt-get autoremove -y && \
    rm -rf /var/lib/apt/lists/*

ENV PATH="/opt/llvm-mingw/bin:${PATH}"

WORKDIR /src

# Cache Go module downloads
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Default: build all targets
CMD ["bash", "build-all.sh"]
