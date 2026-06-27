package service

import (
	"regexp"
	"strconv"
	"strings"
)

func mteamCodeOK(code any) bool {
	codeStr := mteamCodeString(code)
	return codeStr == "0" || codeStr == "200"
}

func mteamCodeString(code any) string {
	switch v := code.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.Itoa(int(v))
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}

// parseSizeString 将带单位的字符串转换为字节数。
func parseSizeString(value string, unit string) int64 {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	switch strings.TrimSuffix(strings.ToLower(unit), "i") {
	case "kb":
		return int64(v * 1024)
	case "mb":
		return int64(v * 1024 * 1024)
	case "gb":
		return int64(v * 1024 * 1024 * 1024)
	case "tb":
		return int64(v * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(v)
	}
}

// stripHTML 移除 HTML 标签。
func stripHTML(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(s, "")
}
