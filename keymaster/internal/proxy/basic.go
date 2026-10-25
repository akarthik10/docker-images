package proxy

import "encoding/base64"

func basic(u, p string) string { return base64.StdEncoding.EncodeToString([]byte(u + ":" + p)) }
