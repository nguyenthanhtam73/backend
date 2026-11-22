package roomquest

import (
	"hash/fnv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// nudgeTemplates are the only sentences a nudge can send.
// {name} is the sender's display name. Clients do not choose the text.
var nudgeTemplates = []string{
	"{name} đang chờ bạn chăm da hôm nay đó 💕",
	"{name} nhắc nhẹ: hôm nay mình chăm da cùng nhau nha ✨",
	"Bé Da của {name} đang vẫy bạn, vào chăm da hôm nay đi 💗",
	"{name} giữ một chỗ trên bảng hôm nay cho bạn đó 🌸",
}

// NudgeMessage picks one fixed sentence for this sender, target, and Vietnam date.
// The same triple always gets the same sentence.
func NudgeMessage(displayName, nickname string, sender, target uuid.UUID, vnDate string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(sender.String()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(target.String()))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(vnDate))
	tmpl := nudgeTemplates[int(h.Sum32())%len(nudgeTemplates)]
	return strings.ReplaceAll(tmpl, "{name}", nudgeName(displayName, nickname))
}

func nudgeName(displayName, nickname string) string {
	name := cleanName(displayName)
	if name == "" {
		name = cleanName(nickname)
	}
	if name == "" {
		return "Một bạn"
	}
	return name
}

func cleanName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, raw)
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" {
		return ""
	}
	if utf8.RuneCountInString(raw) > 24 {
		raw = string([]rune(raw)[:24])
	}
	return raw
}
