FROM copyparty/iv:1.20.16

# The upstream iv image ships a patent-stripped custom ffmpeg (HEVC/H.265 removed)
# and drops vips-heif. We replace it with the standard Alpine ffmpeg and restore
# vips-heif so that HEIC/HEIF thumbnails work.
RUN apk del ffmpeg ffmpeg-libs 2>/dev/null || true && \
    apk add --no-cache --force-overwrite ffmpeg vips-heif
