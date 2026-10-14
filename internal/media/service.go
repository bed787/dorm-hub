package media

import (
	"bytes"
	"context"
	"dorm-hub/internal/domain"
	"dorm-hub/internal/platform/filestore"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"log/slog"
	"math"
	"os"
	"time"
)

const MaxFileSize = 5 * 1024 * 1024

type Service struct {
	db         *pgxpool.Pool
	files      *filestore.Storage
	processing chan struct{}
	uploads    chan struct{}
	quotaBytes int64
	photoLimit int
}

func New(db *pgxpool.Pool, files *filestore.Storage) *Service {
	return NewWithLimits(db, files, 200*1024*1024, 200)
}
func NewWithLimits(db *pgxpool.Pool, files *filestore.Storage, quotaBytes int64, photoLimit int) *Service {
	return &Service{db: db, files: files, processing: make(chan struct{}, 1), uploads: make(chan struct{}, 4), quotaBytes: quotaBytes, photoLimit: photoLimit}
}

// Measure legacy files in small batches so they count towards the same quota.
// Missing files are charged conservatively until their metadata is deleted.
func (s *Service) BackfillSizes(ctx context.Context, logger *slog.Logger) error {
	for {
		rows, err := s.db.Query(ctx, `SELECT id,filename FROM photos WHERE size_bytes=0 ORDER BY id LIMIT 100`)
		if err != nil {
			return err
		}
		type legacy struct{ id, name string }
		batch := []legacy{}
		for rows.Next() {
			var item legacy
			if err = rows.Scan(&item.id, &item.name); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, item := range batch {
			size, err := s.files.Size(item.name)
			if err != nil {
				if !os.IsNotExist(err) {
					return err
				}
				logger.Warn("missing legacy photo file", "photo_id", item.id)
				size = MaxFileSize
			}
			if _, err = s.db.Exec(ctx, `UPDATE photos SET size_bytes=$2 WHERE id=$1 AND size_bytes=0`, item.id, max(int64(1), size)); err != nil {
				return err
			}
		}
	}
}
func (s *Service) normalize(ctx context.Context, data []byte) ([]byte, int, int, error) {
	select {
	case s.processing <- struct{}{}:
		defer func() { <-s.processing }()
	case <-ctx.Done():
		return nil, 0, 0, ctx.Err()
	}
	return Process(data)
}
func Process(data []byte) ([]byte, int, int, error) {
	if len(data) == 0 || len(data) > MaxFileSize {
		return nil, 0, 0, domain.Invalid("Размер фотографии должен быть не больше 5 МБ")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, 0, 0, domain.Invalid("Допустимы фотографии JPEG и PNG")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 12000 || cfg.Height > 12000 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return nil, 0, 0, domain.Invalid("Изображение должно содержать не больше 16 мегапикселей")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, domain.Invalid("Не удалось прочитать изображение")
	}
	scale := math.Min(1, 1920/float64(max(cfg.Width, cfg.Height)))
	width := max(1, int(float64(cfg.Width)*scale))
	height := max(1, int(float64(cfg.Height)*scale))
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := source.Bounds()
	sample := func(x, y int) (float64, float64, float64) {
		x = min(max(x, 0), cfg.Width-1) + bounds.Min.X
		y = min(max(y, 0), cfg.Height-1) + bounds.Min.Y
		r, g, b, a := source.At(x, y).RGBA()
		return float64(r+65535-a) / 257, float64(g+65535-a) / 257, float64(b+65535-a) / 257
	}
	for y := 0; y < height; y++ {
		sy := (float64(y)+.5)*float64(cfg.Height)/float64(height) - .5
		y0 := int(math.Floor(sy))
		dy := sy - float64(y0)
		for x := 0; x < width; x++ {
			sx := (float64(x)+.5)*float64(cfg.Width)/float64(width) - .5
			x0 := int(math.Floor(sx))
			dx := sx - float64(x0)
			r0, g0, b0 := sample(x0, y0)
			r1, g1, b1 := sample(x0+1, y0)
			r2, g2, b2 := sample(x0, y0+1)
			r3, g3, b3 := sample(x0+1, y0+1)
			mix := func(a, b, c, d float64) uint8 { return uint8(math.Round((a*(1-dx)+b*dx)*(1-dy) + (c*(1-dx)+d*dx)*dy)) }
			result.SetRGBA(x, y, color.RGBA{R: mix(r0, r1, r2, r3), G: mix(g0, g1, g2, g3), B: mix(b0, b1, b2, b3), A: 255})
		}
	}
	var output bytes.Buffer
	if err = jpeg.Encode(&output, result, &jpeg.Options{Quality: 85}); err != nil {
		return nil, 0, 0, err
	}
	return output.Bytes(), width, height, nil
}
func (s *Service) Open(ctx context.Context, id string, viewer int64, admin bool) (*os.File, time.Time, error) {
	name, created, err := s.find(ctx, id, viewer, admin)
	if err != nil {
		return nil, time.Time{}, err
	}
	file, err := s.files.Open(name)
	if os.IsNotExist(err) {
		return nil, time.Time{}, domain.ErrNotFound
	}
	return file, created, err
}
func (s *Service) RemoveFiles(names []string) error {
	var first error
	for _, name := range names {
		if err := s.files.Delete(name); err != nil && first == nil {
			first = err
		}
	}
	return first
}
