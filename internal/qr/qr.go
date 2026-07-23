// Package qr генерирует QR-код (PNG) из произвольного текста — обычно из ссылки vless://.
package qr

import qrcode "github.com/skip2/go-qrcode"

// PNG кодирует text в PNG заданного размера (в пикселях). size<=0 → 320.
func PNG(text string, size int) ([]byte, error) {
	if size <= 0 {
		size = 320
	}
	return qrcode.Encode(text, qrcode.Medium, size)
}

// ASCII возвращает QR как текст для вывода в терминал (после установки/add-user).
func ASCII(text string) (string, error) {
	q, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", err
	}
	return q.ToSmallString(false), nil
}
