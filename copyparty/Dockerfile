FROM copyparty/iv:1.20.16

# The upstream iv image ships a patent-stripped custom ffmpeg (HEVC/H.265 removed)
# and drops vips-heif. We replace it with the standard Alpine ffmpeg and restore
# vips-heif so that HEIC/HEIF thumbnails work.
RUN apk del ffmpeg ffmpeg-libavcodec ffmpeg-libavdevice ffmpeg-libavfilter \
        ffmpeg-libavformat ffmpeg-libavutil ffmpeg-libswresample ffmpeg-libswscale \
        2>/dev/null || true && \
    apk add --no-cache ffmpeg vips-heif
