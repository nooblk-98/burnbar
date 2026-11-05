package layout

// Rect matches the Win32 RECT memory layout.
type Rect struct{ Left, Top, Right, Bottom int32 }

// PlacePopup keeps a tray card inside the working area, including monitors with
// negative origins. Top taskbars open downwards; all other edges open upwards.
func PlacePopup(work, anchor Rect, width, height, gap int) (bounds Rect, tailTop bool) {
	width = min(max(width, 1), int(work.Right-work.Left))
	height = min(max(height, 1), int(work.Bottom-work.Top))
	x := int(anchor.Left+anchor.Right)/2 - width/2
	x = max(int(work.Left), min(x, int(work.Right)-width))
	tailTop = anchor.Bottom <= work.Top
	y := int(anchor.Top) - gap - height
	if tailTop {
		y = int(anchor.Bottom) + gap
	}
	y = max(int(work.Top), min(y, int(work.Bottom)-height))
	return Rect{int32(x), int32(y), int32(x + width), int32(y + height)}, tailTop
}
