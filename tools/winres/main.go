// Command winres writes cmd/rosmarops/rsrc_windows_amd64.syso: a COFF x86-64
// object with a .rsrc section holding the program's icon (RT_GROUP_ICON +
// RT_ICON, from assets/windows/rosmarops.ico) and its version information
// (RT_VERSION / VS_VERSIONINFO), so the released rosmarops.exe shows the
// rosemary icon and a Properties → Details version (BV-149).
//
// It has no third-party dependencies (standard library only) and its output is
// byte-for-byte deterministic: nothing in it depends on the time or the
// machine. Usage, from the repository root:
//
//	go run ./tools/winres -version 0.7.2
//
// The .syso it writes is a build input picked up automatically by
// `GOOS=windows GOARCH=amd64 go build ./cmd/rosmarops`; it is git-ignored.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	defaultICO = "assets/windows/rosmarops.ico"
	defaultOut = "cmd/rosmarops/rsrc_windows_amd64.syso"
)

func main() {
	version := flag.String("version", "", "program version without the leading v, e.g. 0.7.2 (required)")
	icoPath := flag.String("ico", defaultICO, "path to the .ico file")
	outPath := flag.String("out", defaultOut, "path to write the .syso object")
	flag.Parse()
	if *version == "" {
		fmt.Fprintln(os.Stderr, "winres: -version is required (e.g. -version 0.7.2)")
		os.Exit(2)
	}
	ico, err := os.ReadFile(*icoPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
	syso, err := buildSyso(ico, *version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, syso, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "winres:", err)
		os.Exit(1)
	}
}

// Resource type ids (Windows RT_* constants).
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	langEN      = 0x0409 // U.S. English
	codepageUni = 0x04B0 // Unicode
)

// resource is one leaf: a (type, id, language) with its bytes.
type resource struct {
	typ, id, lang int
	data          []byte
}

// buildSyso returns the bytes of the COFF object for the given .ico and version.
func buildSyso(ico []byte, version string) ([]byte, error) {
	icons, group, err := parseICO(ico)
	if err != nil {
		return nil, err
	}
	verData, err := versionInfo(version)
	if err != nil {
		return nil, err
	}

	// The leaves, in the order they are laid out: every icon image (RT_ICON,
	// ids 1..N), then the group (RT_GROUP_ICON id 1), then the version
	// (RT_VERSION id 1). Within the directory the types and ids are sorted
	// ascending, which this order already satisfies (3 < 14 < 16, ids 1..N).
	var res []resource
	for i, img := range icons {
		res = append(res, resource{rtIcon, i + 1, langEN, img})
	}
	res = append(res, resource{rtGroupIcon, 1, langEN, group})
	res = append(res, resource{rtVersion, 1, langEN, verData})

	rsrc, relocs, symbolOffsets := buildRsrc(res)
	return assembleCOFF(rsrc, relocs, symbolOffsets), nil
}

// --- .ico parsing -----------------------------------------------------------

// parseICO returns the raw image bytes of each entry and the RT_GROUP_ICON
// directory that references them by id 1..N.
func parseICO(ico []byte) (images [][]byte, group []byte, err error) {
	if len(ico) < 6 {
		return nil, nil, fmt.Errorf("ico too short")
	}
	if le16(ico[0:]) != 0 || le16(ico[2:]) != 1 {
		return nil, nil, fmt.Errorf("not an icon file")
	}
	count := int(le16(ico[4:]))
	if count == 0 {
		return nil, nil, fmt.Errorf("ico has no images")
	}
	if len(ico) < 6+count*16 {
		return nil, nil, fmt.Errorf("ico directory truncated")
	}
	var grp bytes.Buffer
	putU16(&grp, 0)             // idReserved
	putU16(&grp, 1)             // idType = icon
	putU16(&grp, uint16(count)) // idCount
	for i := 0; i < count; i++ {
		e := ico[6+i*16 : 6+i*16+16]
		bWidth, bHeight, bColorCount, bReserved := e[0], e[1], e[2], e[3]
		wPlanes := le16(e[4:])
		wBitCount := le16(e[6:])
		size := le32(e[8:])
		offset := le32(e[12:])
		if int(offset)+int(size) > len(ico) || size == 0 {
			return nil, nil, fmt.Errorf("ico image %d out of range", i)
		}
		images = append(images, ico[offset:offset+size])
		// GRPICONDIRENTRY: the directory entry without the file offset, plus the
		// RT_ICON resource id (1-based, matching the order above).
		grp.WriteByte(bWidth)
		grp.WriteByte(bHeight)
		grp.WriteByte(bColorCount)
		grp.WriteByte(bReserved)
		putU16(&grp, wPlanes)
		putU16(&grp, wBitCount)
		putU32(&grp, size)
		putU16(&grp, uint16(i+1)) // nID
	}
	return images, grp.Bytes(), nil
}

// --- VS_VERSIONINFO ---------------------------------------------------------

// vBlock is one node of the version-info tree (VS_VERSIONINFO and its
// children), marshalled with the wLength/wValueLength/wType header, a
// UTF-16 key and 32-bit alignment.
type vBlock struct {
	key        string
	valueBytes []byte // raw value (nil for container blocks)
	valueLen   int    // wValueLength field: bytes for binary values, WCHARs for strings
	isText     bool   // wType: 1 for text, 0 for binary
	children   []*vBlock
}

func (b *vBlock) marshal() []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0, 0}) // wLength, patched at the end
	putU16(&buf, uint16(b.valueLen))
	wType := uint16(0)
	if b.isText {
		wType = 1
	}
	putU16(&buf, wType)
	buf.Write(utf16z(b.key))
	pad4(&buf)
	if b.valueBytes != nil {
		buf.Write(b.valueBytes)
	}
	// wLength covers the header, key, value and children, but NOT any padding
	// after the last of them: the next sibling's 32-bit alignment is added by the
	// parent (below, before each child). Counting a trailing pad here would
	// overstate wLength and VerQueryValue could not walk the tree.
	for _, c := range b.children {
		pad4(&buf) // align this child on a 32-bit boundary
		buf.Write(c.marshal())
	}
	out := buf.Bytes()
	binary.LittleEndian.PutUint16(out[0:2], uint16(len(out)))
	return out
}

// versionInfo builds the RT_VERSION resource for the given version. The binary
// FileVersion/ProductVersion are major.minor.patch.0; the string fields use the
// version verbatim (without the leading v).
func versionInfo(version string) ([]byte, error) {
	major, minor, patch, err := parseVersion(version)
	if err != nil {
		return nil, err
	}
	var ffi bytes.Buffer
	putU32(&ffi, 0xFEEF04BD)                      // dwSignature
	putU32(&ffi, 0x00010000)                      // dwStrucVersion
	putU32(&ffi, uint32(major)<<16|uint32(minor)) // dwFileVersionMS
	putU32(&ffi, uint32(patch)<<16)               // dwFileVersionLS (build 0)
	putU32(&ffi, uint32(major)<<16|uint32(minor)) // dwProductVersionMS
	putU32(&ffi, uint32(patch)<<16)               // dwProductVersionLS
	putU32(&ffi, 0)                               // dwFileFlagsMask
	putU32(&ffi, 0)                               // dwFileFlags
	putU32(&ffi, 0x00000004)                      // dwFileOS = VOS__WINDOWS32
	putU32(&ffi, 0x00000001)                      // dwFileType = VFT_APP
	putU32(&ffi, 0)                               // dwFileSubtype
	putU32(&ffi, 0)                               // dwFileDateMS
	putU32(&ffi, 0)                               // dwFileDateLS

	str := func(key, val string) *vBlock {
		return &vBlock{
			key:        key,
			valueBytes: utf16z(val),
			valueLen:   len(utf16.Encode([]rune(val))) + 1, // WCHARs incl. null
			isText:     true,
		}
	}
	// The strings, in the order of the brief; no CompanyName.
	stringTable := &vBlock{
		key:    fmt.Sprintf("%04X%04X", langEN, codepageUni),
		isText: true,
		children: []*vBlock{
			str("ProductName", "RosmarOps"),
			str("FileDescription", "RosmarOps"),
			str("LegalCopyright", "© 2026 Albert Garcia Pujadas. AGPL-3.0"),
			str("OriginalFilename", "rosmarops.exe"),
			str("ProductVersion", version),
			str("FileVersion", version),
		},
	}
	stringFileInfo := &vBlock{key: "StringFileInfo", isText: true, children: []*vBlock{stringTable}}

	var trans bytes.Buffer
	putU16(&trans, langEN)
	putU16(&trans, codepageUni)
	varFileInfo := &vBlock{
		key:    "VarFileInfo",
		isText: true,
		children: []*vBlock{{
			key:        "Translation",
			valueBytes: trans.Bytes(),
			valueLen:   trans.Len(), // bytes, binary value
			isText:     false,
		}},
	}

	root := &vBlock{
		key:        "VS_VERSION_INFO",
		valueBytes: ffi.Bytes(),
		valueLen:   ffi.Len(), // bytes, binary value
		isText:     false,
		children:   []*vBlock{stringFileInfo, varFileInfo},
	}
	return root.marshal(), nil
}

// parseVersion reads the leading major.minor.patch from a version string,
// ignoring any -prerelease suffix. Each number must fit in 16 bits.
func parseVersion(v string) (major, minor, patch int, err error) {
	core := v
	if i := strings.IndexByte(core, '-'); i >= 0 {
		core = core[:i]
	}
	if i := strings.IndexByte(core, '+'); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("version %q is not major.minor.patch", v)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, e := strconv.Atoi(p)
		if e != nil || n < 0 || n > 0xFFFF {
			return 0, 0, 0, fmt.Errorf("version %q: bad number %q", v, p)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nil
}

// --- .rsrc section ----------------------------------------------------------

// reloc is one COFF relocation against the .rsrc section.
type reloc struct {
	virtualAddress uint32 // offset of the OffsetToData field within the section
	symbolIndex    uint32
}

// buildRsrc lays out the resource directory (3 levels: type, id, language),
// the data-entry table and the data blobs, and returns the section bytes, the
// relocations for each data entry's OffsetToData field, and the within-section
// offset of each data blob (the value for its symbol).
func buildRsrc(res []resource) (section []byte, relocs []reloc, symbolOffsets []uint32) {
	// Group leaves by type (sorted) then id (sorted). The caller already passes
	// them in that order, so index them directly.
	type idNode struct {
		id   int
		leaf int // index into res
	}
	type typeNode struct {
		typ int
		ids []idNode
	}
	var types []typeNode
	for i, r := range res {
		if len(types) == 0 || types[len(types)-1].typ != r.typ {
			types = append(types, typeNode{typ: r.typ})
		}
		t := &types[len(types)-1]
		t.ids = append(t.ids, idNode{id: r.id, leaf: i})
	}

	const dirHdr = 16
	const dirEnt = 8
	const dataEntSize = 16

	// Offsets of each directory, in layout order: root, then the type subdirs,
	// then the id (language-level) subdirs.
	rootSize := dirHdr + len(types)*dirEnt
	off := rootSize
	typeDirOff := make([]int, len(types))
	for i, t := range types {
		typeDirOff[i] = off
		off += dirHdr + len(t.ids)*dirEnt
	}
	idDirOff := make([][]int, len(types))
	for i, t := range types {
		idDirOff[i] = make([]int, len(t.ids))
		for j := range t.ids {
			idDirOff[i][j] = off
			off += dirHdr + dirEnt // one language entry each
		}
	}
	dataEntriesOff := off
	nLeaves := len(res)
	blobsOff := dataEntriesOff + nLeaves*dataEntSize

	// Lay out the blobs (8-byte aligned); record each leaf's blob offset.
	leafBlobOff := make([]uint32, nLeaves)
	blobCur := blobsOff
	for i := range res {
		leafBlobOff[i] = uint32(blobCur)
		blobCur += align(len(res[i].data), 8)
	}

	var buf bytes.Buffer
	// Root directory.
	writeDir(&buf, len(types))
	for i, t := range types {
		putU32(&buf, uint32(t.typ))
		putU32(&buf, uint32(typeDirOff[i])|0x80000000) // subdirectory
	}
	// Type subdirectories.
	for i, t := range types {
		writeDir(&buf, len(t.ids))
		for j, id := range t.ids {
			putU32(&buf, uint32(id.id))
			putU32(&buf, uint32(idDirOff[i][j])|0x80000000)
		}
	}
	// Id (language-level) subdirectories: one language entry pointing to the
	// data entry.
	leafOrder := 0
	for i, t := range types {
		for range t.ids {
			writeDir(&buf, 1)
			putU32(&buf, uint32(res[typeFirstLeaf(res, i, leafOrder)].lang))
			putU32(&buf, uint32(dataEntriesOff+leafOrder*dataEntSize)) // data entry (high bit clear)
			leafOrder++
		}
	}
	// Data entries. OffsetToData holds the blob's offset within the section; a
	// relocation against the section adds the section's RVA at link time, so the
	// final value is the blob's RVA. (The linker uses the field as the addend and
	// the symbol only to name the section, so the offset goes in the field here,
	// not in the symbol's value.)
	for i := 0; i < nLeaves; i++ {
		fieldVA := uint32(dataEntriesOff + i*dataEntSize)
		relocs = append(relocs, reloc{virtualAddress: fieldVA, symbolIndex: uint32(i)})
		symbolOffsets = append(symbolOffsets, leafBlobOff[i])
		putU32(&buf, leafBlobOff[i])           // OffsetToData (addend; RVA added by reloc)
		putU32(&buf, uint32(len(res[i].data))) // Size
		putU32(&buf, 0)                        // CodePage
		putU32(&buf, 0)                        // Reserved
	}
	// Data blobs, 8-byte aligned.
	for i := range res {
		if got := uint32(buf.Len()); got != leafBlobOff[i] {
			// Defensive: layout math must match the writer.
			panic(fmt.Sprintf("blob %d at %d, expected %d", i, got, leafBlobOff[i]))
		}
		buf.Write(res[i].data)
		for buf.Len()%8 != 0 {
			buf.WriteByte(0)
		}
	}
	return buf.Bytes(), relocs, symbolOffsets
}

// typeFirstLeaf returns the res index of the leafOrder-th leaf; the leaves are
// stored in res in the same order they are laid out.
func typeFirstLeaf(res []resource, _ int, leafOrder int) int { return leafOrder }

func writeDir(buf *bytes.Buffer, nIDEntries int) {
	putU32(buf, 0)                  // Characteristics
	putU32(buf, 0)                  // TimeDateStamp (deterministic)
	putU16(buf, 0)                  // MajorVersion
	putU16(buf, 0)                  // MinorVersion
	putU16(buf, 0)                  // NumberOfNamedEntries
	putU16(buf, uint16(nIDEntries)) // NumberOfIdEntries
}

// --- COFF object ------------------------------------------------------------

const (
	imageFileMachineAMD64 = 0x8664
	relAMD64ADDR32NB      = 0x0003
	scnCntInitData        = 0x00000040
	scnMemRead            = 0x40000000
	symClassStatic        = 3
)

func assembleCOFF(rsrc []byte, relocs []reloc, symbolOffsets []uint32) []byte {
	const fileHdr = 20
	const sectHdr = 40
	const relSize = 10

	nsyms := len(symbolOffsets)
	rawStart := fileHdr + sectHdr
	relStart := rawStart + len(rsrc)
	symStart := relStart + len(relocs)*relSize

	var buf bytes.Buffer
	// IMAGE_FILE_HEADER.
	putU16(&buf, imageFileMachineAMD64)
	putU16(&buf, 1) // NumberOfSections
	putU32(&buf, 0) // TimeDateStamp (deterministic)
	putU32(&buf, uint32(symStart))
	putU32(&buf, uint32(nsyms))
	putU16(&buf, 0) // SizeOfOptionalHeader
	putU16(&buf, 0) // Characteristics

	// Section header for .rsrc.
	name := [8]byte{}
	copy(name[:], ".rsrc")
	buf.Write(name[:])
	putU32(&buf, 0)                 // VirtualSize
	putU32(&buf, 0)                 // VirtualAddress
	putU32(&buf, uint32(len(rsrc))) // SizeOfRawData
	putU32(&buf, uint32(rawStart))  // PointerToRawData
	putU32(&buf, uint32(relStart))  // PointerToRelocations
	putU32(&buf, 0)                 // PointerToLinenumbers
	putU16(&buf, uint16(len(relocs)))
	putU16(&buf, 0) // NumberOfLinenumbers
	putU32(&buf, scnCntInitData|scnMemRead)

	// Section raw data.
	buf.Write(rsrc)

	// Relocations.
	for _, r := range relocs {
		putU32(&buf, r.virtualAddress)
		putU32(&buf, r.symbolIndex)
		putU16(&buf, relAMD64ADDR32NB)
	}

	// Symbol table: one static symbol per data entry, named $R%06d, pointing at
	// the section base (Value 0). The relocation adds this symbol's RVA (the
	// section RVA) to the OffsetToData field, which already holds the blob's
	// offset within the section.
	for i := range symbolOffsets {
		var sn [8]byte
		copy(sn[:], fmt.Sprintf("$R%06d", i))
		buf.Write(sn[:])
		putU32(&buf, 0) // Value (section base)
		putU16(&buf, 1) // SectionNumber (.rsrc)
		putU16(&buf, 0) // Type
		buf.WriteByte(symClassStatic)
		buf.WriteByte(0) // NumberOfAuxSymbols
	}

	// String table: just its 4-byte length (empty; every name fits in 8 bytes).
	putU32(&buf, 4)

	return buf.Bytes()
}

// --- little-endian helpers --------------------------------------------------

func le16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

func putU16(buf *bytes.Buffer, v uint16) {
	var b [2]byte
	binary.LittleEndian.PutUint16(b[:], v)
	buf.Write(b[:])
}

func putU32(buf *bytes.Buffer, v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	buf.Write(b[:])
}

func utf16z(s string) []byte {
	u := utf16.Encode([]rune(s))
	var buf bytes.Buffer
	for _, c := range u {
		putU16(&buf, c)
	}
	putU16(&buf, 0) // null terminator
	return buf.Bytes()
}

func pad4(buf *bytes.Buffer) {
	for buf.Len()%4 != 0 {
		buf.WriteByte(0)
	}
}

func align(n, to int) int { return (n + to - 1) / to * to }
