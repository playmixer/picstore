package rest

import "strings"

type stringList string

// Config конфигурация REST сервиса.
type Config struct {
	Addr           string     `env:"SERVER_ADDRESS envDefault:":8080"`
	BaseURL        string     `env:"SERVER_BASEURL"`
	CookieDomain   stringList `env:"COOKIE_DOMAIN" envDefault:"localhost"`
	CookieSecure   bool       `env:"COOKIE_SECURE" envDefault:"false"`
	CookieLifeTime int        `env:"COOKIE_LIFETIME" envDefault:"0"`
	SSOAuthURL     string     `env:"SSO_AUTH_URL"`
	SSOAuthCert    string     `evn:"SSO_AUTH_CERT_FILE"`
}

func (s stringList) List() []string {
	split := strings.Split(string(s), ";")
	return split
}
