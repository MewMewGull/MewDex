package hub

import (
	"errors"
	"mewdex/internal/protocol"
	"net"
	"net/http"
	"time"
)

type loginWindow struct {
	Started time.Time
	Count   int
}

func (h *Hub) serverInfo(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.store.List("mewdex_accounts")
	if err != nil {
		problem(w, 503, errors.New("中央后端认证信息暂时无法读取"))
		return
	}
	auth := protocol.ServerAuthentication{Methods: []string{"password", "key"}, DefaultMethod: "password", AccountConfigured: len(accounts) > 0}
	if !auth.AccountConfigured {
		auth.Methods = []string{"key", "pair"}
		auth.DefaultMethod = "key"
	}
	reply(w, 200, protocol.ServerInfo{Name: "MewDex", ProtocolVersion: protocol.Version, Capabilities: []string{"mewdex-accounts", "central-projects"}, Authentication: auth})
}

func (h *Hub) login(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	now := time.Now()
	h.mu.Lock()
	if h.logins == nil {
		h.logins = map[string]loginWindow{}
	}
	for key, entry := range h.logins {
		if now.Sub(entry.Started) > time.Minute {
			delete(h.logins, key)
		}
	}
	entry := h.logins[host]
	blocked := entry.Count >= 10 || len(h.logins) >= 512 && entry.Count == 0
	if !blocked {
		if entry.Count == 0 {
			entry.Started = now
		}
		entry.Count++
		h.logins[host] = entry
	}
	h.mu.Unlock()
	if blocked {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, errors.New("登录尝试较多，请稍后重试"))
		return
	}
	var input protocol.MewDexAccountLogin
	if !decode(w, r, &input) {
		return
	}
	token, id, err := h.store.LoginMewDex(input.Username, input.Password)
	if err != nil {
		problem(w, 401, errors.New("MewDex 用户名或密码不正确"))
		return
	}
	reply(w, 200, map[string]any{"token": token, "identity": id, "protocolVersion": protocol.Version})
}

func (h *Hub) mewdexAccount(w http.ResponseWriter, r *http.Request) {
	actor := identity(r)
	if r.Method == "GET" {
		account, err := h.store.MewDexAccount(actor.ID)
		if err != nil {
			problem(w, 500, err)
			return
		}
		reply(w, 200, account)
		return
	}
	if r.Method != "PUT" {
		problem(w, 405, errors.New("unsupported account operation"))
		return
	}
	var input struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	account, err := h.store.SetMewDexAccount(actor, input.Username, input.Name, input.Password)
	if err != nil {
		problem(w, 400, err)
		return
	}
	reply(w, 200, account)
}
