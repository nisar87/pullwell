// Command mkres writes rsrc_windows_amd64.syso: the program icon (several sizes) and the version information that
// Explorer, the taskbar, Alt+Tab and Task Manager show. It needs no tools outside Go: it draws the icon and writes
// the COFF object that the Go linker takes in by itself.
//
//	go run ./scripts/mkres            (from the repository root; run again when VERSION changes)
//
// The file is not kept in the repository (it is generated): the workflows and scripts/drop-update.sh run this
// before building, so the version shown in Explorer always matches VERSION.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

func main() {
	src, err := os.ReadFile("appname.go")
	if err != nil {
		fail("run this from the repository root: %v", err)
	}
	name := find(src, `const AppName = "([^"]+)"`)
	ver := strings.TrimSpace(string(must(os.ReadFile("VERSION"))))
	if find(src, `appVersion = "([^"]+)"`) != ver {
		fail("VERSION (%s) and appVersion in appname.go differ", ver)
	}
	out := build(name, ver)
	if err := os.WriteFile("rsrc_windows_amd64.syso", out, 0o644); err != nil {
		fail("%v", err)
	}
	if len(os.Args) > 1 && os.Args[1] == "-ico" { // also write the .ico file (for the README or a shortcut)
		_ = os.WriteFile(strings.ToLower(name)+".ico", icoFile(), 0o644)
	}
	fmt.Printf("rsrc_windows_amd64.syso: %s %s, %d bytes\n", name, ver, len(out))
}

func fail(f string, a ...any) { fmt.Fprintf(os.Stderr, "mkres: "+f+"\n", a...); os.Exit(1) }
func must(b []byte, err error) []byte {
	if err != nil {
		fail("%v", err)
	}
	return b
}
func find(src []byte, re string) string {
	m := regexp.MustCompile(re).FindSubmatch(src)
	if m == nil {
		fail("not found in appname.go: %s", re)
	}
	return string(m[1])
}

// ---------- the icon ----------

var sizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

// draw paints the icon at n x n: a teal disc with a white arrow pointing down into a tray line, antialiased by
// sampling each pixel 4x4 times. The same shape as the tray icon, drawn more carefully.
func draw(n int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	const ss = 4
	blue := [3]float64{13, 141, 128} // the flow teal of the window
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var cover, white float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					fx := (float64(x) + (float64(sx)+0.5)/ss) / float64(n) // 0..1
					fy := (float64(y) + (float64(sy)+0.5)/ss) / float64(n)
					dx, dy := fx-0.5, fy-0.5
					if dx*dx+dy*dy > 0.47*0.47 {
						continue
					}
					cover++
					shaft := fx >= 0.44 && fx < 0.56 && fy >= 0.2 && fy < 0.58
					hy := fy - 0.48 // the head: a triangle from y .48 to .70
					head := hy >= 0 && fy < 0.70 && math.Abs(fx-0.5) < 0.24-hy*(0.24/0.22)
					tray := fy >= 0.75 && fy < 0.81 && fx >= 0.27 && fx < 0.73
					if shaft || head || tray {
						white++
					}
				}
			}
			if cover == 0 {
				continue
			}
			w := white / cover
			c := func(i int) uint8 { return uint8(math.Round(blue[i]*(1-w) + 255*w)) }
			img.SetNRGBA(x, y, color.NRGBA{c(0), c(1), c(2), uint8(math.Round(cover / (ss * ss) * 255))})
		}
	}
	return img
}

// iconImage is one RT_ICON resource: PNG for 256 (as Windows expects), a 32-bit DIB for the small sizes.
func iconImage(n int) []byte {
	img := draw(n)
	if n >= 256 {
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		return b.Bytes()
	}
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	// BITMAPINFOHEADER; the height counts the colour image and the mask
	le(uint32(40))
	le(int32(n))
	le(int32(2 * n))
	le(uint16(1))
	le(uint16(32))
	le(uint32(0))
	le(uint32(n * n * 4))
	le(int32(0))
	le(int32(0))
	le(uint32(0))
	le(uint32(0))
	for y := n - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < n; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	stride := ((n + 31) / 32) * 4
	for y := n - 1; y >= 0; y-- { // AND mask: 1 = transparent
		row := make([]byte, stride)
		for x := 0; x < n; x++ {
			if img.NRGBAAt(x, y).A == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(row)
	}
	return b.Bytes()
}

func groupIcon(imgs [][]byte) []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	le(uint16(0))
	le(uint16(1))
	le(uint16(len(sizes)))
	for i, n := range sizes {
		d := uint8(n)
		if n >= 256 {
			d = 0
		}
		b.Write([]byte{d, d, 0, 0})
		le(uint16(1))
		le(uint16(32))
		le(uint32(len(imgs[i])))
		le(uint16(i + 1))
	}
	return b.Bytes()
}

// icoFile is the same icon as a .ico file.
func icoFile() []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	imgs := make([][]byte, len(sizes))
	for i, n := range sizes {
		imgs[i] = iconImage(n)
	}
	le(uint16(0))
	le(uint16(1))
	le(uint16(len(sizes)))
	off := 6 + 16*len(sizes)
	for i, n := range sizes {
		d := uint8(n)
		if n >= 256 {
			d = 0
		}
		b.Write([]byte{d, d, 0, 0})
		le(uint16(1))
		le(uint16(32))
		le(uint32(len(imgs[i])))
		le(uint32(off))
		off += len(imgs[i])
	}
	for _, im := range imgs {
		b.Write(im)
	}
	return b.Bytes()
}

// ---------- version information ----------

func u16z(s string) []byte {
	var b bytes.Buffer
	for _, r := range utf16.Encode([]rune(s)) {
		_ = binary.Write(&b, binary.LittleEndian, r)
	}
	b.Write([]byte{0, 0})
	return b.Bytes()
}

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

// vnode writes one block of the version resource: wLength, wValueLength, wType, the key, the value, the children.
func vnode(key string, value []byte, valueLen uint16, text bool, children ...[]byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0, 0, 0, 0, 0, 0})
	b.Write(u16z(key))
	pad4(&b)
	b.Write(value)
	for _, c := range children {
		pad4(&b)
		b.Write(c)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint16(out[0:], uint16(len(out)))
	binary.LittleEndian.PutUint16(out[2:], valueLen)
	t := uint16(0)
	if text {
		t = 1
	}
	binary.LittleEndian.PutUint16(out[4:], t)
	return out
}

func versionInfo(name, ver string) []byte {
	var nums [4]uint16
	for i, p := range strings.SplitN(ver, ".", 4) {
		v, _ := strconv.Atoi(p)
		nums[i] = uint16(v)
	}
	var fixed bytes.Buffer
	le := func(v any) { _ = binary.Write(&fixed, binary.LittleEndian, v) }
	le(uint32(0xFEEF04BD))
	le(uint32(0x00010000))
	le(uint32(nums[0])<<16 | uint32(nums[1]))
	le(uint32(nums[2])<<16 | uint32(nums[3]))
	le(uint32(nums[0])<<16 | uint32(nums[1]))
	le(uint32(nums[2])<<16 | uint32(nums[3]))
	le(uint32(0x3F)) // flags mask
	le(uint32(0))    // flags
	le(uint32(0x40004))
	le(uint32(1)) // VFT_APP
	le(uint32(0))
	le(uint32(0))
	le(uint32(0))
	str := func(k, v string) []byte {
		val := u16z(v)
		return vnode(k, val, uint16(len(val)/2), true)
	}
	strs := vnode("040904b0", nil, 0, true,
		str("CompanyName", name),
		str("FileDescription", name),
		str("FileVersion", ver),
		str("InternalName", name),
		str("LegalCopyright", "MIT licence"),
		str("OriginalFilename", name+".exe"),
		str("ProductName", name),
		str("ProductVersion", ver),
	)
	sfi := vnode("StringFileInfo", nil, 0, true, strs)
	tr := []byte{0x09, 0x04, 0xb0, 0x04}
	vfi := vnode("VarFileInfo", nil, 0, true, vnode("Translation", tr, 4, false))
	return vnode("VS_VERSION_INFO", fixed.Bytes(), uint16(fixed.Len()), false, sfi, vfi)
}

// ---------- the COFF object ----------

type res struct {
	typ, id uint32
	data    []byte
}

func build(name, ver string) []byte {
	var rs []res
	imgs := make([][]byte, len(sizes))
	for i, n := range sizes {
		imgs[i] = iconImage(n)
		rs = append(rs, res{3, uint32(i + 1), imgs[i]}) // RT_ICON
	}
	rs = append(rs, res{14, 1, groupIcon(imgs)})        // RT_GROUP_ICON
	rs = append(rs, res{16, 1, versionInfo(name, ver)}) // RT_VERSION

	// the tree: root -> type -> id -> language (0x409) -> data entry
	var types []uint32
	byType := map[uint32][]res{}
	for _, r := range rs {
		if _, ok := byType[r.typ]; !ok {
			types = append(types, r.typ)
		}
		byType[r.typ] = append(byType[r.typ], r)
	}
	dirSize := func(n int) int { return 16 + 8*n }
	// sizes of all directories, to place the data entries after them
	total := dirSize(len(types))
	for _, t := range types {
		total += dirSize(len(byType[t]))
		total += dirSize(1) * len(byType[t])
	}
	dataEntries := total
	blobs := dataEntries + 16*len(rs)

	sec := make([]byte, 0, 1<<16)
	put := func(v any) {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.LittleEndian, v)
		sec = append(sec, b.Bytes()...)
	}
	dir := func(n int) {
		put(uint32(0))
		put(uint32(0))
		put(uint16(0))
		put(uint16(0))
		put(uint16(0))
		put(uint16(n))
	}

	// offsets: the root, then each type directory, then each id directory, then each language directory
	typeDir := map[uint32]int{}
	off := dirSize(len(types))
	for _, t := range types {
		typeDir[t] = off
		off += dirSize(len(byType[t]))
	}
	idDir := map[[2]uint32]int{}
	for _, t := range types {
		for _, r := range byType[t] {
			idDir[[2]uint32{t, r.id}] = off
			off += dirSize(1)
		}
	}
	dir(len(types))
	for _, t := range types {
		put(t)
		put(uint32(typeDir[t]) | 0x80000000)
	}
	for _, t := range types {
		dir(len(byType[t]))
		for _, r := range byType[t] {
			put(r.id)
			put(uint32(idDir[[2]uint32{t, r.id}]) | 0x80000000)
		}
	}
	k := 0
	for _, t := range types {
		for range byType[t] {
			dir(1)
			put(uint32(0x409))
			put(uint32(dataEntries + 16*k)) // a leaf: the data entry
			k++
		}
	}
	if len(sec) != dataEntries {
		fail("layout error")
	}
	// data entries (their first field is relocated to the image address of the data) and the data itself
	var relocs []uint32
	pos := blobs
	var order []res
	for _, t := range types {
		order = append(order, byType[t]...)
	}
	for _, r := range order {
		relocs = append(relocs, uint32(len(sec)))
		put(uint32(pos))
		put(uint32(len(r.data)))
		put(uint32(0))
		put(uint32(0))
		pos += (len(r.data) + 7) &^ 7
	}
	for _, r := range order {
		sec = append(sec, r.data...)
		for len(sec)%8 != 0 {
			sec = append(sec, 0)
		}
	}

	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	const hdr, shdr = 20, 40
	rawAt := hdr + shdr
	relAt := rawAt + len(sec)
	symAt := relAt + 10*len(relocs)
	// COFF file header
	le(uint16(0x8664))
	le(uint16(1))
	le(uint32(0))
	le(uint32(symAt))
	le(uint32(1))
	le(uint16(0))
	le(uint16(0))
	// section header
	b.Write([]byte(".rsrc\x00\x00\x00"))
	le(uint32(0))
	le(uint32(0))
	le(uint32(len(sec)))
	le(uint32(rawAt))
	le(uint32(relAt))
	le(uint32(0))
	le(uint16(len(relocs)))
	le(uint16(0))
	le(uint32(0x40000040)) // initialised data, readable
	b.Write(sec)
	for _, r := range relocs {
		le(r)
		le(uint32(0)) // symbol 0: the section
		le(uint16(3)) // IMAGE_REL_AMD64_ADDR32NB
	}
	// symbol table: the section symbol, then an empty string table
	b.Write([]byte(".rsrc\x00\x00\x00"))
	le(uint32(0))
	le(int16(1))
	le(uint16(0))
	b.WriteByte(3) // IMAGE_SYM_CLASS_STATIC
	b.WriteByte(0)
	le(uint32(4))
	return b.Bytes()
}
