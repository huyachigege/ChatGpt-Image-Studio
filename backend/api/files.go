package api

import (
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"chatgpt2api/internal/users"
)

const (
	defaultImageDir           = "data/tmp/image"
	imageThumbnailMaxDim      = 720
	imageThumbnailJPEGQuality = 88
)

// downloadAndCache downloads an upstream image using the image client's transport
// (Chrome TLS fingerprint), saves to local disk, and returns the local filename.
func downloadAndCache(client imageDownloader, upstreamURL string, cacheDir string) (string, error) {
	// Generate a stable filename from the URL
	hash := sha256.Sum256([]byte(upstreamURL))
	filename := fmt.Sprintf("%x.png", hash[:12])
	dir := firstNonEmpty(cacheDir, defaultImageDir)
	localPath := filepath.Join(dir, filename)

	// Check cache
	if _, err := os.Stat(localPath); err == nil {
		return filename, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	data, err := client.DownloadBytes(upstreamURL)
	if err != nil {
		return "", fmt.Errorf("download upstream image: %w", err)
	}

	tmpFile := localPath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmpFile, localPath); err != nil {
		return "", err
	}

	slog.Info("cached image", "file", filename, "size", len(data))
	return filename, nil
}

// gatewayImageURL builds the public URL for a cached image.
func gatewayImageURL(r *http.Request, filename string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	return fmt.Sprintf("%s://%s/v1/files/image/%s", scheme, host, strings.TrimLeft(filename, "/"))
}

func (s *Server) resolveImageFilePath(name string) string {
	cleaned := strings.Trim(strings.TrimSpace(name), "/")
	parts := strings.Split(cleaned, "/")
	baseName := filepath.Base(cleaned)
	imageRoot := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	candidates := []string{}
	if len(parts) >= 2 {
		userID := strings.ReplaceAll(parts[0], "\\", "-")
		fileName := filepath.Base(parts[len(parts)-1])
		candidates = append(candidates, filepath.Join(imageRoot, userID, fileName))
		baseName = fileName
	}
	candidates = append(candidates, filepath.Join(imageRoot, baseName))
	legacyPath := filepath.Join(s.cfg.ResolvePath(defaultImageDir), baseName)
	if len(candidates) == 0 || !strings.EqualFold(filepath.Clean(legacyPath), filepath.Clean(candidates[len(candidates)-1])) {
		candidates = append(candidates, legacyPath)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return s.searchImageFilePathFallback(baseName)
}

func (s *Server) searchImageFilePathFallback(name string) string {
	baseName := filepath.Base(strings.TrimSpace(name))
	if baseName == "" || s == nil || s.cfg == nil {
		return ""
	}

	dataRoot := filepath.Join(s.cfg.Paths().Root, "data")
	info, err := os.Stat(dataRoot)
	if err != nil || !info.IsDir() {
		return ""
	}

	var found string
	_ = filepath.WalkDir(dataRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || found != "" {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(entry.Name(), baseName) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		found = path
		return fs.SkipAll
	})
	return found
}

// handleImageFile serves cached and server-stored images from storage.image_dir.
func (s *Server) handleImageFile(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/files/image/"), "/")
	if name == "" {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}

	path := s.resolveImageFilePath(name)
	if path == "" {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}

	s.serveImageFile(w, r, path)
}

func (s *Server) handleImageThumbnail(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/files/image-thumb/"), "/")
	if name == "" {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}

	path := s.resolveImageFilePath(name)
	if path == "" {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}

	thumbPath, err := s.ensureImageThumbnail(path)
	if err != nil {
		slog.Warn("serve original image because thumbnail generation failed", "file", filepath.Base(path), "error", err)
		s.serveImageFile(w, r, path)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, thumbPath)
}

func (s *Server) serveImageFile(w http.ResponseWriter, r *http.Request, path string) {
	ext := strings.ToLower(filepath.Ext(path))
	contentTypes := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".webp": "image/webp",
		".gif":  "image/gif",
	}
	ct := contentTypes[ext]
	if ct == "" {
		ct = "image/png"
	}

	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", ct)
	limiter := s.imageDownloadRateLimiter(r)
	if limiter == nil {
		http.ServeFile(w, r, path)
		return
	}
	serveRateLimitedFile(w, r, path, limiter)
}

func (s *Server) imageDownloadRateLimiter(r *http.Request) *rateLimiter {
	if s == nil || s.cfg == nil {
		return nil
	}
	identity, ok := s.imageIdentityFromRequest(r)
	if ok && identity.Role == users.RoleAdmin {
		return nil
	}
	kbps := s.cfg.Server.ImageDownloadRateKBPerSecond
	if kbps <= 0 {
		return nil
	}
	key := "anonymous"
	if ok && strings.TrimSpace(identity.UserID) != "" {
		key = "user:" + strings.TrimSpace(identity.UserID)
	}
	s.imageDownloadLimitersMu.Lock()
	defer s.imageDownloadLimitersMu.Unlock()
	if s.imageDownloadLimiters == nil {
		s.imageDownloadLimiters = map[string]*rateLimiter{}
	}
	bytesPerSecond := int64(kbps) * 1024
	limiter := s.imageDownloadLimiters[key]
	if limiter == nil || limiter.rate != bytesPerSecond {
		limiter = newRateLimiter(bytesPerSecond)
		s.imageDownloadLimiters[key] = limiter
	}
	return limiter
}

func serveRateLimitedFile(w http.ResponseWriter, r *http.Request, path string, limiter *rateLimiter) {
	file, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), &rateLimitedReadSeeker{ReadSeeker: file, limiter: limiter})
}

type rateLimiter struct {
	mu        sync.Mutex
	rate      int64
	capacity  int64
	available int64
	updated   time.Time
}

func newRateLimiter(bytesPerSecond int64) *rateLimiter {
	if bytesPerSecond < 1 {
		bytesPerSecond = 1
	}
	return &rateLimiter{rate: bytesPerSecond, capacity: bytesPerSecond, available: bytesPerSecond, updated: time.Now()}
}

func (l *rateLimiter) wait(n int) {
	if l == nil || n <= 0 {
		return
	}
	remaining := int64(n)
	for remaining > 0 {
		l.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(l.updated)
		if elapsed > 0 {
			l.available += int64(float64(l.rate) * elapsed.Seconds())
			if l.available > l.capacity {
				l.available = l.capacity
			}
			l.updated = now
		}
		if l.available > 0 {
			take := remaining
			if take > l.available {
				take = l.available
			}
			l.available -= take
			remaining -= take
			l.mu.Unlock()
			continue
		}
		wait := time.Duration(remaining) * time.Second / time.Duration(l.rate)
		if wait < 10*time.Millisecond {
			wait = 10 * time.Millisecond
		}
		l.mu.Unlock()
		time.Sleep(wait)
	}
}

type rateLimitedReadSeeker struct {
	io.ReadSeeker
	limiter *rateLimiter
}

func (r *rateLimitedReadSeeker) Read(p []byte) (int, error) {
	if r == nil || r.limiter == nil {
		return r.ReadSeeker.Read(p)
	}
	if int64(len(p)) > r.limiter.rate/4 && r.limiter.rate >= 4 {
		p = p[:r.limiter.rate/4]
	}
	n, err := r.ReadSeeker.Read(p)
	if n > 0 {
		r.limiter.wait(n)
	}
	return n, err
}

func (s *Server) ensureImageThumbnail(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	cachePath := s.imageThumbnailCachePath(path, info)
	if cached, err := os.Stat(cachePath); err == nil && cached.Mode().IsRegular() {
		return cachePath, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	src, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}
	thumb := resizeImageNearest(src, imageThumbnailMaxDim)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", err
	}
	tmpFile := cachePath + ".tmp"
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", err
	}
	encodeErr := jpeg.Encode(out, thumb, &jpeg.Options{Quality: imageThumbnailJPEGQuality})
	closeErr := out.Close()
	if encodeErr != nil {
		_ = os.Remove(tmpFile)
		return "", encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpFile)
		return "", closeErr
	}
	if err := os.Rename(tmpFile, cachePath); err != nil {
		_ = os.Remove(tmpFile)
		return "", err
	}
	return cachePath, nil
}

func (s *Server) imageThumbnailCachePath(path string, info os.FileInfo) string {
	cacheRoot := filepath.Join(s.cfg.ResolvePath(s.cfg.Storage.ImageDir), ".thumbs")
	fingerprint := fmt.Sprintf("%s:%d:%d:%d:%d", filepath.Clean(path), info.ModTime().UnixNano(), info.Size(), imageThumbnailMaxDim, imageThumbnailJPEGQuality)
	hash := sha256.Sum256([]byte(fingerprint))
	return filepath.Join(cacheRoot, fmt.Sprintf("%x.jpg", hash[:12]))
}

func (s *Server) removeImageThumbnail(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	_ = os.Remove(s.imageThumbnailCachePath(path, info))
}

func resizeImageNearest(src image.Image, maxDim int) *image.RGBA {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	if maxDim <= 0 {
		maxDim = imageThumbnailMaxDim
	}
	dstW, dstH := width, height
	if width > height && width > maxDim {
		dstW = maxDim
		dstH = max(1, height*maxDim/width)
	} else if height >= width && height > maxDim {
		dstH = maxDim
		dstW = max(1, width*maxDim/height)
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		sy := bounds.Min.Y + y*height/dstH
		for x := 0; x < dstW; x++ {
			sx := bounds.Min.X + x*width/dstW
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
