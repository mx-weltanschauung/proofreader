package opds

import (
	"encoding/xml"
	"io"
	"time"
)

// Типы ссылок и ответов. kind различает навигационную ленту (её записи ведут
// в другие ленты) и ленту книг (у записей есть файлы) — так их делит OPDS 1.2.
const (
	typeNavigation  = "application/atom+xml;profile=opds-catalog;kind=navigation"
	typeAcquisition = "application/atom+xml;profile=opds-catalog;kind=acquisition"
	typeOpenSearch  = "application/opensearchdescription+xml"
	typeEPUB        = "application/epub+zip"
	typeFB2         = "application/x-fictionbook+xml"
	typeHTML        = "text/html"
	typePNG         = "image/png"

	relAcquisition = "http://opds-spec.org/acquisition/open-access"
	relImage       = "http://opds-spec.org/image"
	relThumbnail   = "http://opds-spec.org/image/thumbnail"
	relSubsection  = "subsection"

	nsAtom = "http://www.w3.org/2005/Atom"
	nsDC   = "http://purl.org/dc/terms/"
	nsOS   = "http://a9.com/-/spec/opensearch/1.1/"
)

// Префиксы dc: и пространства имён пишутся буквально, а не через
// пространство имён в теге encoding/xml: тот объявляет xmlns на каждом
// элементе заново, а часть читалок ищет именно «dc:language».
type feed struct {
	XMLName xml.Name `xml:"feed"`
	Xmlns   string   `xml:"xmlns,attr"`
	XmlnsDC string   `xml:"xmlns:dc,attr"`
	ID      string   `xml:"id"`
	Title   string   `xml:"title"`
	Updated string   `xml:"updated"`
	Author  author   `xml:"author"`
	Links   []link   `xml:"link"`
	Entries []entry  `xml:"entry"`
}

type author struct {
	Name string `xml:"name"`
	URI  string `xml:"uri,omitempty"`
}

type link struct {
	Rel   string `xml:"rel,attr,omitempty"`
	Href  string `xml:"href,attr"`
	Type  string `xml:"type,attr,omitempty"`
	Title string `xml:"title,attr,omitempty"`
}

type entry struct {
	ID       string   `xml:"id"`
	Title    string   `xml:"title"`
	Updated  string   `xml:"updated"`
	Authors  []author `xml:"author"`
	Language string   `xml:"dc:language,omitempty"`
	Issued   string   `xml:"dc:issued,omitempty"`
	Summary  string   `xml:"summary,omitempty"`
	Links    []link   `xml:"link"`
}

type openSearch struct {
	XMLName     xml.Name `xml:"OpenSearchDescription"`
	Xmlns       string   `xml:"xmlns,attr"`
	ShortName   string   `xml:"ShortName"`
	Description string   `xml:"Description"`
	InputEnc    string   `xml:"InputEncoding"`
	OutputEnc   string   `xml:"OutputEncoding"`
	URL         osURL    `xml:"Url"`
}

type osURL struct {
	Type     string `xml:"type,attr"`
	Template string `xml:"template,attr"`
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func writeXML(w io.Writer, v any) error {
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
