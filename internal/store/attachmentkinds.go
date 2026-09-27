package store

import (
	"mime"
	"path/filepath"
	"strings"
)

// What the chat draws an attachment as (docs/webui.md 4.21).
const (
	AttachmentImage   = "image"
	AttachmentVideo   = "video"
	AttachmentAudio   = "audio"
	AttachmentPDF     = "pdf"
	AttachmentText    = "text"
	AttachmentOffice  = "office"
	AttachmentArchive = "archive"
	AttachmentOther   = "other"
)

// AttachmentKinds are every kind, in the order the attachments tab's
// filters group them: pictures and video, documents, the rest.
var AttachmentKinds = []string{
	AttachmentImage, AttachmentVideo, AttachmentPDF, AttachmentText, AttachmentOffice, AttachmentAudio, AttachmentArchive, AttachmentOther,
}

// AttachmentKindOf says what an upload is from its media type, and from
// its name where a browser sends no telling type: code and office files
// often come as plain text or bytes.
func AttachmentKindOf(mediaType, filename string) string {
	mt, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		mt = mediaType
	}
	mt = strings.ToLower(mt)
	ext := strings.ToLower(filepath.Ext(filename))
	switch {
	case strings.HasPrefix(mt, "image/"):
		return AttachmentImage
	// Sound in an MP4 box sniffs as video/mp4: its name tells it apart.
	case strings.HasPrefix(mt, "audio/") || audioExts[ext]:
		return AttachmentAudio
	case strings.HasPrefix(mt, "video/"):
		return AttachmentVideo
	case mt == "application/pdf" || ext == ".pdf":
		return AttachmentPDF
	// Office files are zips inside: their names decide before the type.
	case officeExts[ext] || officeTypes[mt] || strings.HasPrefix(mt, "application/vnd.openxmlformats-officedocument.") ||
		strings.HasPrefix(mt, "application/vnd.oasis.opendocument."):
		return AttachmentOffice
	case archiveExts[ext] || archiveTypes[mt]:
		return AttachmentArchive
	case strings.HasPrefix(mt, "text/") || textTypes[mt] || textExts[ext]:
		return AttachmentText
	}
	return AttachmentOther
}

var audioExts = set(".m4a", ".mp3", ".wav", ".aac", ".ogg", ".oga", ".opus", ".flac", ".weba")

var officeExts = set(".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".odt", ".ods", ".odp", ".rtf", ".pages", ".numbers", ".key")

var officeTypes = set("application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint", "application/rtf")

var archiveExts = set(".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".7z", ".rar", ".zst")

var archiveTypes = set("application/zip", "application/x-zip-compressed", "application/x-tar", "application/gzip", "application/x-gzip",
	"application/x-bzip2", "application/x-xz", "application/x-7z-compressed", "application/vnd.rar", "application/x-rar-compressed", "application/zstd")

var textTypes = set("application/json", "application/xml", "application/javascript", "application/x-sh", "application/x-yaml", "application/yaml",
	"application/toml", "application/sql", "application/x-ndjson")

var textExts = set(".txt", ".md", ".markdown", ".log", ".csv", ".tsv", ".json", ".jsonl", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf",
	".xml", ".html", ".htm", ".css", ".scss", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".go", ".py", ".rb", ".rs", ".java", ".kt",
	".swift", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".php", ".sh", ".bash", ".zsh", ".fish", ".sql", ".proto", ".graphql", ".vue",
	".svelte", ".lua", ".pl", ".r", ".scala", ".dart", ".ex", ".exs", ".erl", ".hs", ".clj", ".diff", ".patch", ".env", ".mod", ".sum")

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}
