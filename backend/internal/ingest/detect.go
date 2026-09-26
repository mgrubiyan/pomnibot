package ingest

import (
	"bytes"
	"net/http"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Kind is what a file is by its content.
type Kind string

// Kinds of input files.
const (
	KindText        Kind = "text"
	KindPDF         Kind = "pdf"
	KindJPEG        Kind = "jpeg"
	KindPNG         Kind = "png"
	KindUnsupported Kind = "unsupported"
)

// DetectKind tells the file type by its content, not its name: a file from a
// messenger may be called anything.
func DetectKind(data []byte) Kind {
	switch ct := http.DetectContentType(data); {
	case ct == "application/pdf":
		return KindPDF
	case ct == "image/jpeg":
		return KindJPEG
	case ct == "image/png":
		return KindPNG
	case strings.HasPrefix(ct, "text/plain"):
		return KindText
	}
	return KindUnsupported
}

// decodeText turns a text file into UTF-8. Besides UTF-8 it reads UTF-16 with
// a byte order mark (Windows Notepad's "Unicode") and falls back to
// Windows-1251, still common for Russian notes: read as UTF-8, such a file
// would become replacement characters and yield no cards. The second value
// names the encoding when it was not UTF-8.
func decodeText(data []byte) (string, string) {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return string(data[3:]), ""
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16(data[2:], false), "UTF-16"
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return decodeUTF16(data[2:], true), "UTF-16"
	case utf8.Valid(data):
		return string(data), ""
	}
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, c := range data {
		if c < 0x80 {
			b.WriteByte(c)
			continue
		}
		b.WriteRune(cp1251[c-0x80])
	}
	return b.String(), "Windows-1251"
}

func decodeUTF16(data []byte, bigEndian bool) string {
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			units = append(units, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return string(utf16.Decode(units))
}

// cp1251 maps bytes 0x80–0xFF of Windows-1251 to Unicode.
var cp1251 = [128]rune{
	'Ђ', 'Ѓ', '‚', 'ѓ', '„', '…', '†', '‡', '€', '‰', 'Љ', '‹', 'Њ', 'Ќ', 'Ћ', 'Џ',
	'ђ', '‘', '’', '“', '”', '•', '–', '—', '\uFFFD', '™', 'љ', '›', 'њ', 'ќ', 'ћ', 'џ',
	'\u00A0', 'Ў', 'ў', 'Ј', '¤', 'Ґ', '¦', '§', 'Ё', '©', 'Є', '«', '¬', '\u00AD', '®', 'Ї',
	'°', '±', 'І', 'і', 'ґ', 'µ', '¶', '·', 'ё', '№', 'є', '»', 'ј', 'Ѕ', 'ѕ', 'ї',
	'А', 'Б', 'В', 'Г', 'Д', 'Е', 'Ж', 'З', 'И', 'Й', 'К', 'Л', 'М', 'Н', 'О', 'П',
	'Р', 'С', 'Т', 'У', 'Ф', 'Х', 'Ц', 'Ч', 'Ш', 'Щ', 'Ъ', 'Ы', 'Ь', 'Э', 'Ю', 'Я',
	'а', 'б', 'в', 'г', 'д', 'е', 'ж', 'з', 'и', 'й', 'к', 'л', 'м', 'н', 'о', 'п',
	'р', 'с', 'т', 'у', 'ф', 'х', 'ц', 'ч', 'ш', 'щ', 'ъ', 'ы', 'ь', 'э', 'ю', 'я',
}
