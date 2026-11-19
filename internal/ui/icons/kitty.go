package icons

import (
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tensleyconsulting/seiner/internal/model"
)

// UploadDelay is how long to wait after startup before uploading images.
// Delay until alt-screen has been entered. tea.Raw from Init races the first
// renderer flush and Ghostty stores images per screen.
const UploadDelay = 200 * time.Millisecond

// KittyRenderer emits Kitty Graphics Protocol sequences plus Unicode
// placeholders so Bubble Tea/Lip Gloss can lay out cells normally.
// PNG bytes are transmitted once per (service, size) and reused by image ID.
// SizeBanner and SizeSplash exist only for Logo, from the larger raster in
// logo; each grid has its own image ID.
type KittyRenderer struct {
	pngs     map[model.ServiceType][]byte
	listIDs  map[model.ServiceType]int
	heroIDs  map[model.ServiceType]int
	logo     []byte
	bannerID int
	splashID int
	logoSent bool
	mu       sync.Mutex
	sent     map[model.ServiceType]bool
	// tmux wraps uploads in tmux's passthrough sequence.
	tmux bool
}

func newKitty(pngs map[model.ServiceType][]byte) *KittyRenderer {
	// IDs follow KnownServices, then any other icon by name, so they are
	// the same on every run.
	listIDs := make(map[model.ServiceType]int)
	heroIDs := make(map[model.ServiceType]int)
	var order []model.ServiceType
	for _, svc := range append(model.KnownServices(), slices.Sorted(maps.Keys(pngs))...) {
		if _, ok := listIDs[svc]; !ok {
			order = append(order, svc)
			listIDs[svc] = len(order)
		}
	}
	i := len(order) + 1
	for _, svc := range order {
		heroIDs[svc] = i
		i++
	}
	return &KittyRenderer{
		pngs:     pngs,
		listIDs:  listIDs,
		heroIDs:  heroIDs,
		bannerID: i,
		splashID: i + 1,
		sent:     map[model.ServiceType]bool{},
	}
}

func (k *KittyRenderer) Name() string { return "kitty" }

func (k *KittyRenderer) Cols(size Size) int { return size.Cols() }

func (k *KittyRenderer) Rows(size Size) int { return size.Rows() }

func (k *KittyRenderer) Upload() string {
	if k == nil {
		return ""
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	var b strings.Builder
	n := 0
	for svc, png := range k.pngs {
		if len(png) == 0 || k.sent[svc] {
			continue
		}
		b.WriteString(transmitPNG(k.listIDs[svc], png, SizeList.Cols(), SizeList.Rows(), k.tmux))
		b.WriteString(transmitPNG(k.heroIDs[svc], png, SizeHero.Cols(), SizeHero.Rows(), k.tmux))
		k.sent[svc] = true
		n++
	}
	if len(k.logo) > 0 && !k.logoSent {
		b.WriteString(transmitPNG(k.bannerID, k.logo, SizeBanner.Cols(), SizeBanner.Rows(), k.tmux))
		b.WriteString(transmitPNG(k.splashID, k.logo, SizeSplash.Cols(), SizeSplash.Rows(), k.tmux))
		k.logoSent = true
		n += 2
	}
	slog.Debug("kitty transmit built", "images", n, "bytes", b.Len())
	return b.String()
}

// Reset forgets what was uploaded, so the next Upload sends every image
// again. A terminal reset or a tmux reattach can drop stored images while
// the placeholders on screen still name them.
func (k *KittyRenderer) Reset() {
	if k == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	clear(k.sent)
	k.logoSent = false
}

// Delete returns sequences that free every image this renderer uploaded,
// in ID order, and forgets them. Images stay in the terminal's memory
// until deleted, so call it on quit, via tea.Raw before tea.Quit (while
// the alt screen they were uploaded on is still active).
func (k *KittyRenderer) Delete() string {
	if k == nil {
		return ""
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	var ids []int
	for svc, sent := range k.sent {
		if sent {
			ids = append(ids, k.listIDs[svc], k.heroIDs[svc])
		}
	}
	if k.logoSent {
		ids = append(ids, k.bannerID, k.splashID)
	}
	slices.Sort(ids)
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(deleteImage(id, k.tmux))
	}
	clear(k.sent)
	k.logoSent = false
	slog.Debug("kitty delete built", "images", len(ids))
	return b.String()
}

func (k *KittyRenderer) RenderAt(service model.ServiceType, size Size, row int) string {
	if k == nil {
		return TextRenderer{}.RenderAt(service, size, row)
	}
	if size == SizeBanner || size == SizeSplash {
		if service != Logo || len(k.logo) == 0 {
			return TextRenderer{}.RenderAt(service, size, row)
		}
	} else if png := k.pngs[service]; len(png) == 0 {
		return TextRenderer{}.RenderAt(service, size, row)
	}
	id, ok := k.id(service, size)
	if !ok {
		return TextRenderer{}.RenderAt(service, size, row)
	}
	if row < 0 || row >= size.Rows() {
		return ""
	}
	// Placeholders only. PNG bytes must go through Upload() → tea.Raw;
	// Bubble Tea v2's cell buffer strips APC if we embed it in View().
	return placeholderRow(id, row, size.Cols())
}

// id returns the image ID for service at size, or false when it has none.
func (k *KittyRenderer) id(service model.ServiceType, size Size) (int, bool) {
	var id int
	switch size {
	case SizeHero:
		id = k.heroIDs[service]
	case SizeBanner:
		if service == Logo {
			id = k.bannerID
		}
	case SizeSplash:
		if service == Logo {
			id = k.splashID
		}
	default:
		id = k.listIDs[service]
	}
	return id, id != 0
}

func (k *KittyRenderer) sentCount() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	n := 0
	for _, v := range k.sent {
		if v {
			n++
		}
	}
	return n
}

var _ Renderer = (*KittyRenderer)(nil)
