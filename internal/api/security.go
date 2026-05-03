package api

import (
	"crypto/subtle"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"

	"mynewmangaui/internal/config"
)

const accessCookieName = "manga_access_token"

type accessControl struct {
	allowPrivateNetworks bool
	publicAccessToken    string
	trustedProxyNets     []*net.IPNet
}

func newAccessControl(cfg config.ServerConfig) (*accessControl, error) {
	ac := &accessControl{
		allowPrivateNetworks: cfg.AllowPrivateNetworks,
		publicAccessToken:    strings.TrimSpace(cfg.PublicAccessToken),
	}

	for _, raw := range cfg.TrustedProxyCIDRs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, err
		}
		ac.trustedProxyNets = append(ac.trustedProxyNets, network)
	}

	return ac, nil
}

func (ac *accessControl) middleware(next http.Handler) http.Handler {
	if ac == nil || (ac.allowPrivateNetworks && ac.publicAccessToken == "") {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ac.isAuthRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := ac.clientIP(r)
		if ac.allowPrivateNetworks && isPrivateIP(clientIP) {
			next.ServeHTTP(w, r)
			return
		}

		if ac.publicAccessToken == "" {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "public access is disabled",
			})
			return
		}

		if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" && ac.matchToken(token) {
			ac.setAccessCookie(w, token, r.TLS != nil)
			next.ServeHTTP(w, r)
			return
		}

		if ac.authorized(r) {
			next.ServeHTTP(w, r)
			return
		}

		if wantsHTML(r) {
			http.Redirect(w, r, "/auth/login?next="+urlQueryEscape(requestURIOrRoot(r)), http.StatusSeeOther)
			return
		}

		w.Header().Set("WWW-Authenticate", `Bearer realm="manga-ui"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "public access requires a valid token",
		})
	})
}

func (ac *accessControl) isAuthRoute(path string) bool {
	return path == "/auth/login" || path == "/auth/logout"
}

func (ac *accessControl) authorized(r *http.Request) bool {
	if cookie, err := r.Cookie(accessCookieName); err == nil && ac.matchToken(cookie.Value) {
		return true
	}

	if token := strings.TrimSpace(r.Header.Get("X-Access-Token")); token != "" && ac.matchToken(token) {
		return true
	}

	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return ac.matchToken(strings.TrimSpace(authHeader[7:]))
	}

	return false
}

func (ac *accessControl) matchToken(token string) bool {
	if ac.publicAccessToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(ac.publicAccessToken)) == 1
}

func (ac *accessControl) clientIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil
	}

	if !ac.isTrustedProxy(ip) {
		return ip
	}

	for _, forwarded := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		forwarded = strings.TrimSpace(forwarded)
		candidate := net.ParseIP(forwarded)
		if candidate != nil {
			return candidate
		}
	}

	if realIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); realIP != nil {
		return realIP
	}

	return ip
}

func (ac *accessControl) isTrustedProxy(ip net.IP) bool {
	for _, network := range ac.trustedProxyNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func (ac *accessControl) setAccessCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   60 * 60 * 24 * 30,
	})
}

func clearAccessCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     accessCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func wantsHTML(r *http.Request) bool {
	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html") {
		return true
	}
	return !strings.HasPrefix(r.URL.Path, "/api/")
}

func requestURIOrRoot(r *http.Request) string {
	if r.URL == nil || strings.TrimSpace(r.URL.RequestURI()) == "" {
		return "/"
	}
	return r.URL.RequestURI()
}

func urlQueryEscape(value string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		" ", "%20",
		"\"", "%22",
		"#", "%23",
		"&", "%26",
		"+", "%2B",
		"?", "%3F",
	)
	return replacer.Replace(value)
}

func (ac *accessControl) loginPage(w http.ResponseWriter, r *http.Request) {
	if ac.authorized(r) {
		http.Redirect(w, r, sanitizeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}

	next := sanitizeNext(r.URL.Query().Get("next"))
	loginPageHTML(w, next, "")
}

func (ac *accessControl) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		loginPageHTML(w, "/", "请求格式不正确，请重新输入访问令牌。")
		return
	}

	token := strings.TrimSpace(r.FormValue("token"))
	next := sanitizeNext(r.FormValue("next"))
	if !ac.matchToken(token) {
		w.WriteHeader(http.StatusUnauthorized)
		loginPageHTML(w, next, "访问令牌无效，请检查后再试。")
		return
	}

	ac.setAccessCookie(w, token, r.TLS != nil)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (ac *accessControl) logout(w http.ResponseWriter, r *http.Request) {
	clearAccessCookie(w)
	http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
}

func sanitizeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return "/"
	}
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

func loginPageHTML(w http.ResponseWriter, next string, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>登录漫画书库</title>
    <style>
      :root {
        --bg: #efe5d5;
        --surface: rgba(255, 252, 247, 0.92);
        --text: #221815;
        --muted: #705d56;
        --line: rgba(60, 36, 26, 0.12);
        --accent: #7a2f1c;
        --shadow: 0 24px 60px rgba(78, 45, 26, 0.14);
      }
      * { box-sizing: border-box; }
      body {
        margin: 0;
        min-height: 100vh;
        display: grid;
        place-items: center;
        padding: 24px;
        font-family: "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif;
        color: var(--text);
        background:
          radial-gradient(circle at top left, rgba(190, 90, 53, 0.18), transparent 28%%),
          radial-gradient(circle at top right, rgba(122, 47, 28, 0.12), transparent 24%%),
          linear-gradient(180deg, #f8f2e9 0%%, var(--bg) 50%%, #eadcc8 100%%);
      }
      .login-card {
        width: min(100%%, 460px);
        padding: 30px 28px;
        border-radius: 28px;
        border: 1px solid var(--line);
        background: var(--surface);
        backdrop-filter: blur(14px);
        box-shadow: var(--shadow);
      }
      .eyebrow {
        font-size: 12px;
        letter-spacing: 0.18em;
        text-transform: uppercase;
        color: var(--accent);
      }
      h1 {
        margin: 14px 0 0;
        font-size: clamp(2rem, 6vw, 3rem);
        line-height: 1.05;
      }
      p {
        margin: 14px 0 0;
        color: var(--muted);
        line-height: 1.7;
      }
      form {
        margin-top: 24px;
        display: grid;
        gap: 14px;
      }
      label {
        display: grid;
        gap: 8px;
        font-size: 14px;
        color: var(--muted);
      }
      input {
        width: 100%%;
        padding: 13px 14px;
        border-radius: 16px;
        border: 1px solid rgba(126, 47, 25, 0.16);
        background: rgba(255, 250, 243, 0.92);
        color: var(--text);
        font-size: 16px;
      }
      button {
        border: 1px solid rgba(126, 47, 25, 0.18);
        background: #fffaf2;
        color: var(--accent);
        border-radius: 999px;
        padding: 12px 18px;
        font-size: 15px;
        font-weight: 600;
        cursor: pointer;
      }
      .error {
        margin-top: 18px;
        padding: 12px 14px;
        border-radius: 16px;
        background: rgba(158, 44, 24, 0.08);
        color: #9e2c18;
        font-size: 14px;
      }
      .note {
        margin-top: 18px;
        font-size: 13px;
        color: var(--muted);
      }
    </style>
  </head>
  <body>
    <main class="login-card">
      <div class="eyebrow">My New Manga UI</div>
      <h1>访问验证</h1>
      <p>局域网访问无需验证。当前请求来自公网，请输入访问令牌继续。令牌只会通过表单提交给服务器，并保存为 HttpOnly Cookie，不会写入前端脚本或本地存储。</p>
      %s
      <form method="post" action="/auth/login" autocomplete="off">
        <input type="hidden" name="next" value="%s" />
        <label>
          访问令牌
          <input name="token" type="password" inputmode="text" autocapitalize="off" autocomplete="current-password" required />
        </label>
        <button type="submit">进入漫画书库</button>
      </form>
      <div class="note">建议通过 HTTPS 或反向代理访问公网入口，以获得更安全的传输保护。</div>
    </main>
  </body>
</html>`, renderError(message), html.EscapeString(next))
}

func renderError(message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	return `<div class="error">` + html.EscapeString(message) + `</div>`
}
