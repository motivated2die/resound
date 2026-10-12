package ui

// shadowSpec is the panel's drop shadow in logical pixels: a soft, slightly
// lowered shadow, like Windows 11 flyouts but a bit stronger.
var shadowSpec = struct {
	blur    int     // blur radius
	offsetY int     // shadow sits this much lower than the panel
	corner  int     // corner radius of the panel (Windows 11 rounds by 8)
	opacity float64 // darkest alpha, 0..1
}{blur: 18, offsetY: 6, corner: 8, opacity: 0.55}

// shadowMask returns the alpha of a blurred rounded rectangle of w×h,
// lowered by offY, inside a canvas padded by pad on every side (pad must be
// at least blur + offY). Three box blurs approximate a Gaussian.
func shadowMask(w, h, pad, blur, offY, corner int, opacity float64) (mask []uint8, cw, ch int) {
	cw, ch = w+2*pad, h+2*pad
	a := make([]float32, cw*ch)
	top := pad + offY
	r := corner
	for y := top; y < top+h; y++ {
		for x := pad; x < pad+w; x++ {
			// Cut the corners outside the radius.
			dx := max(pad+r-x-1, x-(pad+w-r), 0)
			dy := max(top+r-y-1, y-(top+h-r), 0)
			if dx*dx+dy*dy <= r*r {
				a[y*cw+x] = 1
			}
		}
	}
	box := max(1, blur/2)
	tmp := make([]float32, len(a))
	for range 3 {
		boxBlur(a, tmp, cw, ch, box, 1, cw) // rows
		boxBlur(tmp, a, ch, cw, box, cw, 1) // columns
	}
	mask = make([]uint8, len(a))
	for i, v := range a {
		mask[i] = uint8(min(255, v*float32(opacity)*255+0.5))
	}
	return mask, cw, ch
}

// boxBlur averages src over a window of 2r+1 samples along lines of n
// samples (step apart), for count lines (stride apart), into dst.
func boxBlur(src, dst []float32, n, count, r, step, stride int) {
	norm := 1 / float32(2*r+1)
	for line := range count {
		base := line * stride
		var sum float32
		for i := -r; i <= r; i++ {
			if i >= 0 && i < n {
				sum += src[base+i*step]
			}
		}
		for i := range n {
			dst[base+i*step] = sum * norm
			if out := i - r; out >= 0 {
				sum -= src[base+out*step]
			}
			if in := i + r + 1; in < n {
				sum += src[base+in*step]
			}
		}
	}
}
