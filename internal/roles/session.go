package roles

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/kpatdev/quota-router/internal/session"
)

const SessionPrefix = "minillm-role:"

// Keep task executions out of desktop conversation bindings. Use a stable,
// non-reversible namespace per role and conversation; never change prompt data.
// Codex's original thread/session cache headers remain intact.
func roleHeaders(req Request, role string) http.Header {
	h := req.Headers.Clone()
	if h == nil {
		h = make(http.Header)
	}
	ref, ok := session.FromRequest(req.Headers, req.Metadata)
	id := ref.Key
	if !ok {
		var random [16]byte
		_, _ = rand.Read(random[:])
		id = hex.EncodeToString(random[:])
	}
	sum := sha256.Sum256([]byte(role + "\x00" + id))
	h.Set("X-Claude-Code-Session-Id", SessionPrefix+hex.EncodeToString(sum[:16]))
	h.Del("X-Claude-Code-Agent-Id")
	h.Del("X-Claude-Code-Parent-Agent-Id")
	return h
}
func IsRoleSession(id string) bool { return strings.HasPrefix(id, "claude:"+SessionPrefix) }
