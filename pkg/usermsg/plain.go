// Package usermsg turns English API error text into plain Vietnamese.
// Error codes and HTTP statuses stay with the caller. This package only
// rewrites the human message.
package usermsg

import (
	"fmt"
	"regexp"
	"strings"
)

// Generic is the plain line used when an English message has no specific
// translation. It avoids leaking jargon (SQL, tokens, provider names).
const Generic = "Có lỗi xảy ra. Bạn thử lại giúp mình nhé."

var (
	reMinutes   = regexp.MustCompile(`(?i)(\d+)\s*minutes`)
	reEachImage = regexp.MustCompile(`(?i)each image must be <=\s*(\d+)\s*MB`)
	reOneImage  = regexp.MustCompile(`(?i)^image must be <=\s*(\d+)\s*MB`)
	reMaxPhotos = regexp.MustCompile(`(?i)maximum\s+(\d+)\s+photos`)
)

// exact maps a full English message to plain Vietnamese.
// Keys are the strings handlers and usecases already return.
var exact = map[string]string{
	"invalid email or password":               "Email hoặc mật khẩu chưa đúng.",
	"email already registered":                "Email này đã được đăng ký rồi.",
	"email already registered for beta":       "Email này đã đăng ký nhận tin rồi.",
	"username already taken":                  "Tên này đã có người dùng.",
	"user account is inactive":                "Tài khoản này đang tạm khoá.",
	"user not found":                          "Không tìm thấy tài khoản.",
	"invalid user":                            "Không tìm thấy tài khoản.",
	"invalid or revoked refresh token":        "Phiên đăng nhập hết hạn. Bạn đăng nhập lại nhé.",
	"authentication misconfigured":            "Đăng nhập đang lỗi. Bạn thử lại sau nhé.",
	"token service misconfigured":             "Đăng nhập đang lỗi. Bạn thử lại sau nhé.",
	"database is not available":               "Hệ thống đang bận. Bạn thử lại sau nhé.",
	"database error":                          "Hệ thống đang bận. Bạn thử lại sau nhé.",
	"something went wrong":                    Generic,
	"invalid email":                           "Email chưa đúng, ví dụ: ten@gmail.com",
	"invalid input":                           "Thông tin chưa đúng. Bạn kiểm tra lại nhé.",
	"invalid password":                        "Mật khẩu chưa đúng.",
	"authentication is not available":         "Đăng nhập đang tạm ngưng. Bạn thử lại sau nhé.",
	"request body must be valid JSON":         "Dữ liệu gửi lên chưa đúng. Bạn thử lại nhé.",
	"body must be valid JSON":                 "Dữ liệu gửi lên chưa đúng. Bạn thử lại nhé.",
	"invalid JSON body":                       "Dữ liệu gửi lên chưa đúng. Bạn thử lại nhé.",
	"captcha verification is required":        "Bạn xác nhận không phải robot rồi thử lại nhé.",
	"captcha verification failed":             "Xác nhận chưa đúng. Bạn thử lại nhé.",
	"could not verify captcha; try again":     "Chưa xác nhận được. Bạn thử lại nhé.",
	"user context missing":                    "Bạn cần đăng nhập lại.",
	"missing user":                            "Bạn cần đăng nhập lại.",
	"missing or invalid Authorization header": "Bạn cần đăng nhập lại.",
	"empty bearer token":                      "Bạn cần đăng nhập lại.",
	"access token invalid or expired":         "Phiên đăng nhập hết hạn. Bạn đăng nhập lại nhé.",
	"JWT service not configured":              "Đăng nhập đang lỗi. Bạn thử lại sau nhé.",
	"Too many requests. Please slow down for a minute and try again.": "Bạn thao tác hơi nhanh. Đợi một phút rồi thử lại nhé.",

	"skin check service is not available":                      "Check-in đang tạm ngưng. Bạn thử lại sau nhé.",
	"expected multipart form data":                             "Dữ liệu gửi lên chưa đúng. Bạn thử lại nhé.",
	`field "climate_context" must be valid JSON when provided`: "Thông tin thời tiết gửi lên chưa đúng.",
	`field "images" must include at least one file`:            "Bạn chọn ít nhất một ảnh.",
	"skip-mode check-in needs at least one tag or note":        "Khi không gửi ảnh, bạn cần chọn một dấu hiệu hoặc viết một dòng ghi chú.",
	"uploaded image is empty (0 bytes)":                        "Ảnh trống. Bạn chọn ảnh khác nhé.",
	"only jpeg, png, webp, gif are allowed":                    "Chỉ nhận ảnh JPEG, PNG, WEBP hoặc GIF.",
	"could not read uploaded image":                            "Không đọc được ảnh. Bạn thử ảnh khác nhé.",
	"uploaded file is not a recognizable image":                "File này không phải ảnh. Bạn chọn ảnh khác nhé.",
	"visibility must be public or private":                     "Chế độ xem chỉ có công khai hoặc riêng tư.",
	"skin check id must be a valid uuid":                       "Không tìm thấy lần check-in này.",
	"skin check not found":                                     "Không tìm thấy lần check-in này.",
	"reanalyze needs at least one stored photo":                "Cần có ảnh đã lưu thì mới phân tích lại được.",
	"this check can be reanalyzed once per day":                "Mỗi ngày chỉ phân tích lại một lần.",
	"could not save skin check":                                "Chưa lưu được check-in. Bạn thử lại nhé.",
	"could not create skin check":                              "Chưa tạo được check-in. Bạn thử lại nhé.",
	"could not load skin check":                                "Chưa tải được lần check-in. Bạn thử lại nhé.",
	"invalid skin check payload":                               "Thông tin check-in chưa đúng. Bạn kiểm tra lại nhé.",
	"content did not pass moderation":                          "Nội dung này không dùng được. Bạn thử ảnh hoặc ghi chú khác nhé.",
	"photos required to reanalyze":                             "Cần có ảnh thì mới phân tích lại được.",
	"reanalyze limit reached":                                  "Mỗi ngày chỉ phân tích lại một lần.",
	`field "photo_meta" must be a JSON array`:                  "Thông tin ảnh gửi lên chưa đúng.",
	"photo_meta has more entries than uploaded images":         "Số mô tả ảnh nhiều hơn số ảnh đã gửi.",
	`field "skin_context" must be a JSON object`:               "Thông tin da gửi lên chưa đúng.",
	"skin_context.extra must be at most 300 characters":        "Phần ghi chú thêm dài quá (tối đa 300 ký tự).",
	"could not read photo context":                             "Không đọc được thông tin ảnh.",

	"monthly free limit reached — upgrade for unlimited access": "Bạn đã hết lượt miễn phí tháng này. Nâng cấp để dùng tiếp nhé.",
	"upgrade your plan to use this feature":                     "Bạn cần nâng cấp gói để dùng tính năng này.",
	"premium gate unavailable":                                  "Tính năng này đang tạm ngưng. Bạn thử lại sau nhé.",
	"premium service unavailable":                               "Tính năng này đang tạm ngưng. Bạn thử lại sau nhé.",
	"feature not available on current plan":                     "Gói hiện tại chưa có tính năng này.",
	"monthly quota exceeded":                                    "Bạn đã hết lượt miễn phí tháng này.",
	"premium required for this action":                          "Bạn cần nâng cấp gói để dùng tính năng này.",

	"invalid sepay secret":                        "Mã thanh toán không khớp.",
	"SePay is not configured":                     "Thanh toán đang tạm ngưng. Bạn thử lại sau nhé.",
	"Self-serve checkout is disabled during Beta": "Thanh toán tự phục vụ đang tắt trong giai đoạn thử nghiệm.",
	"payment service unavailable":                 "Thanh toán đang tạm ngưng. Bạn thử lại sau nhé.",
	"payment unavailable":                         "Thanh toán đang tạm ngưng. Bạn thử lại sau nhé.",
	"payment order not found":                     "Không tìm thấy đơn thanh toán.",
	"paid amount does not cover order":            "Số tiền chưa đủ so với đơn.",
	"payment processing failed":                   "Thanh toán chưa xong. Bạn thử lại nhé.",
	"invalid payment request":                     "Thông tin thanh toán chưa đúng. Bạn kiểm tra lại nhé.",

	"subscription service unavailable": "Gói đăng ký đang tạm ngưng. Bạn thử lại sau nhé.",
	"subscription unavailable":         "Gói đăng ký đang tạm ngưng. Bạn thử lại sau nhé.",
	"not eligible for trial":           "Bạn không còn lượt dùng thử.",
	"no active subscription to cancel": "Không có gói đang dùng để huỷ.",
	"subscription already canceled":    "Gói này đã được huỷ rồi.",
	"invalid plan tier":                "Gói bạn chọn chưa đúng.",
	"subscription request failed":      "Chưa xử lý được gói. Bạn thử lại nhé.",

	"unknown event":                   "Mình không nhận loại sự kiện này.",
	"props is too large":              "Phần thông tin kèm theo dài quá.",
	"props must be a flat object":     "Thông tin kèm theo chưa đúng dạng.",
	"session_id is too long":          "Mã phiên dài quá.",
	"session_id is required":          "Thiếu mã phiên.",
	"session_id is invalid":           "Mã phiên chưa đúng.",
	"path is too long":                "Đường dẫn dài quá.",
	"body is too large":               "Nội dung gửi lên dài quá.",
	"invalid error_type":              "Loại lỗi chưa đúng.",
	"props must not include an email": "Đừng gửi kèm email.",
	"unknown prop":                    "Có mục thông tin mình không nhận.",
	"event is required":               "Thiếu tên sự kiện.",
	"client_ts must be ISO8601":       "Thời gian gửi lên chưa đúng.",

	"feedback unavailable":                          "Phần góp ý đang tạm ngưng. Bạn thử lại sau nhé.",
	"feedback service unavailable":                  "Phần góp ý đang tạm ngưng. Bạn thử lại sau nhé.",
	"feedback not found":                            "Không tìm thấy góp ý này.",
	"reminder unavailable":                          "Phần nhắc nhở đang tạm ngưng. Bạn thử lại sau nhé.",
	"could not load reminder":                       "Chưa tải được lời nhắc. Bạn thử lại nhé.",
	"could not refresh reminders":                   "Chưa làm mới được lời nhắc. Bạn thử lại nhé.",
	"dashboard unavailable":                         "Trang tổng quan đang tạm ngưng. Bạn thử lại sau nhé.",
	"dashboard service unavailable":                 "Trang tổng quan đang tạm ngưng. Bạn thử lại sau nhé.",
	"could not load dashboard":                      "Chưa tải được trang tổng quan. Bạn thử lại nhé.",
	"could not load streak":                         "Chưa tải được chuỗi ngày. Bạn thử lại nhé.",
	"could not load usage":                          "Chưa tải được lượt dùng. Bạn thử lại nhé.",
	"could not load progress":                       "Chưa tải được tiến trình. Bạn thử lại nhé.",
	"unsubscribe unavailable":                       "Huỷ nhận thư đang tạm ngưng. Bạn thử lại sau nhé.",
	"unsubscribe link is invalid":                   "Liên kết huỷ nhận thư không còn dùng được.",
	"could not unsubscribe":                         "Chưa huỷ nhận thư được. Bạn thử lại nhé.",
	"beta signup unavailable":                       "Đăng ký nhận tin đang tạm ngưng. Bạn thử lại sau nhé.",
	"beta signup service unavailable":               "Đăng ký nhận tin đang tạm ngưng. Bạn thử lại sau nhé.",
	"could not save beta signup":                    "Chưa lưu được đăng ký. Bạn thử lại nhé.",
	"affiliate unavailable":                         "Liên kết sản phẩm đang tạm ngưng. Bạn thử lại sau nhé.",
	"affiliate service unavailable":                 "Liên kết sản phẩm đang tạm ngưng. Bạn thử lại sau nhé.",
	"memory service unavailable":                    "Mình chưa mở được phần DaDiary nhớ về bạn. Bạn thử lại nhé.",
	"profile service unavailable":                   "Hồ sơ đang tạm ngưng. Bạn thử lại sau nhé.",
	"invalid profile payload":                       "Thông tin hồ sơ chưa đúng. Bạn kiểm tra lại nhé.",
	"onboarding profile not found":                  "Chưa có hồ sơ da. Bạn làm bước làm quen trước nhé.",
	"routine service unavailable":                   "Chu trình đang tạm ngưng. Bạn thử lại sau nhé.",
	"invalid routine payload":                       "Thông tin chu trình chưa đúng. Bạn kiểm tra lại nhé.",
	"wardrobe service unavailable":                  "Tủ đồ đang tạm ngưng. Bạn thử lại sau nhé.",
	"invalid product payload":                       "Thông tin sản phẩm chưa đúng. Bạn kiểm tra lại nhé.",
	"wardrobe product not found":                    "Không tìm thấy sản phẩm này.",
	"wardrobe shelf cap exceeded":                   "Tủ đồ đã đầy.",
	"streak service unavailable":                    "Chuỗi ngày đang tạm ngưng. Bạn thử lại sau nhé.",
	"no streak freezes available":                   "Bạn không còn lượt đóng băng chuỗi ngày.",
	"a freeze is already active for the target day": "Ngày này đã được đóng băng rồi.",
	"no active streak to protect":                   "Chưa có chuỗi ngày để bảo vệ.",
	"streak already broken; cannot use a freeze":    "Chuỗi ngày đã đứt, không đóng băng được nữa.",
	"check in today to continue your streak":        "Bạn check-in hôm nay để nối chuỗi ngày nhé.",
	"usage service unavailable":                     "Lượt dùng đang tạm ngưng. Bạn thử lại sau nhé.",
	"paywall view service unavailable":              "Trang nâng cấp đang tạm ngưng. Bạn thử lại sau nhé.",
	"push subscription service unavailable":         "Thông báo đang tạm ngưng. Bạn thử lại sau nhé.",
	"push sender unavailable":                       "Thông báo đang tạm ngưng. Bạn thử lại sau nhé.",
	"user data service unavailable":                 "Dữ liệu tài khoản đang tạm ngưng. Bạn thử lại sau nhé.",
	"invalid user id":                               "Không tìm thấy tài khoản.",
	"account deletion schema is not ready":          "Chưa xoá được tài khoản lúc này. Bạn thử lại sau nhé.",
	"account not found":                             "Không tìm thấy tài khoản.",

	"admin access required":                      "Bạn không có quyền quản trị.",
	"skin review access required":                "Bạn không có quyền xem phần này.",
	"admin auth not configured":                  "Phần quản trị đang lỗi. Bạn thử lại sau nhé.",
	"could not verify admin":                     "Chưa kiểm tra được quyền quản trị.",
	"admin activity unavailable":                 "Phần hoạt động đang tạm ngưng. Bạn thử lại sau nhé.",
	"could not load activity":                    "Chưa tải được hoạt động. Bạn thử lại nhé.",
	"date must be YYYY-MM-DD":                    "Ngày phải theo dạng năm-tháng-ngày.",
	"admin funnel stats unavailable":             "Phần thống kê đang tạm ngưng. Bạn thử lại sau nhé.",
	"could not load funnel stats":                "Chưa tải được thống kê. Bạn thử lại nhé.",
	"admin metrics unavailable":                  "Phần số liệu đang tạm ngưng. Bạn thử lại sau nhé.",
	"could not load payment metrics":             "Chưa tải được số liệu thanh toán. Bạn thử lại nhé.",
	"could not load affiliate metrics":           "Chưa tải được số liệu liên kết. Bạn thử lại nhé.",
	"admin skin review unavailable":              "Phần soi da đang tạm ngưng. Bạn thử lại sau nhé.",
	"admin skin review not found":                "Không tìm thấy bản soi da này.",
	"upload 1 skin photo (required)":             "Bạn cần tải lên 1 ảnh da.",
	"maximum 3 photos for admin skin review":     "Phần soi da nhận tối đa 3 ảnh.",
	"OpenAI API key required for photo analysis": "Chưa phân tích được ảnh lúc này. Bạn thử lại sau nhé.",
	"could not analyze photo":                    "Chưa phân tích được ảnh. Bạn thử lại nhé.",
	"could not complete admin skin review":       "Chưa hoàn thành bản soi da. Bạn thử lại nhé.",
	"id must be a valid UUID":                    "Mã này chưa đúng.",
	"user id must be a valid UUID":               "Mã tài khoản chưa đúng.",
	"admin users unavailable":                    "Danh sách tài khoản đang tạm ngưng. Bạn thử lại sau nhé.",
	"could not search users":                     "Chưa tìm được tài khoản. Bạn thử lại nhé.",
	"could not load user":                        "Chưa tải được tài khoản. Bạn thử lại nhé.",
	"user already has this plan":                 "Tài khoản đang dùng gói này rồi.",
	"could not update plan":                      "Chưa đổi được gói. Bạn thử lại nhé.",

	"Analysis timed out. Please try submitting again.":                 "Phân tích hơi lâu nên dừng lại. Bạn gửi lại giúp mình nhé.",
	"OpenAI API key missing (required for photo analysis vision pass)": "Mình chưa phân tích được ảnh lúc này. Bạn thử lại sau nhé.",
	"could not reload analysis after coach run":                        "Chưa lưu được kết quả. Bạn thử lại nhé.",
	"could not save completed analysis":                                "Chưa lưu được kết quả. Bạn thử lại nhé.",
	"circuit breaker open: AI provider temporarily unavailable":        "Mình đang bận. Bạn thử lại sau ít phút nhé.",
	"check-in reminder service unavailable":                            "Phần nhắc check-in đang tạm ngưng. Bạn thử lại sau nhé.",
}

// prefixes cover wrapped errors such as "invalid input: user id required".
// Longer prefixes must come first.
var prefixes = []struct {
	prefix string
	vi     string
}{
	{"invalid skin check payload", "Thông tin check-in chưa đúng. Bạn kiểm tra lại nhé."},
	{"content did not pass moderation", "Nội dung này không dùng được. Bạn thử ảnh hoặc ghi chú khác nhé."},
	{"invalid payment request", "Thông tin thanh toán chưa đúng. Bạn kiểm tra lại nhé."},
	{"invalid profile payload", "Thông tin hồ sơ chưa đúng. Bạn kiểm tra lại nhé."},
	{"invalid routine payload", "Thông tin chu trình chưa đúng. Bạn kiểm tra lại nhé."},
	{"invalid product payload", "Thông tin sản phẩm chưa đúng. Bạn kiểm tra lại nhé."},
	{"invalid admin skin review input", "Thông tin soi da chưa đúng. Bạn kiểm tra lại nhé."},
	{"memory service unavailable", "Mình chưa mở được phần DaDiary nhớ về bạn. Bạn thử lại nhé."},
	{"invalid input", "Thông tin chưa đúng. Bạn kiểm tra lại nhé."},
	{"database error", "Hệ thống đang bận. Bạn thử lại sau nhé."},
}

// Plain returns msg in plain Vietnamese when it is user-facing English.
// A message that is already Vietnamese is returned unchanged.
// Calling Plain twice returns the same text.
func Plain(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	if vi, ok := exact[msg]; ok {
		return vi
	}
	if vi, ok := matchPattern(msg); ok {
		return vi
	}
	if containsVietnamese(msg) {
		return msg
	}
	if vi, ok := matchPrefix(msg); ok {
		return vi
	}
	if looksLikeEnglish(msg) {
		return Generic
	}
	return msg
}

func matchPattern(msg string) (string, bool) {
	low := strings.ToLower(msg)
	if strings.Contains(low, "too many attempts") {
		n := "vài"
		if m := reMinutes.FindStringSubmatch(msg); len(m) == 2 {
			n = m[1]
		}
		return fmt.Sprintf("Bạn thử nhiều lần quá. Đợi %s phút rồi thử lại nhé.", n), true
	}
	if m := reEachImage.FindStringSubmatch(msg); len(m) == 2 {
		return fmt.Sprintf("Mỗi ảnh tối đa %s MB.", m[1]), true
	}
	if m := reOneImage.FindStringSubmatch(msg); len(m) == 2 {
		return fmt.Sprintf("Ảnh tối đa %s MB.", m[1]), true
	}
	if m := reMaxPhotos.FindStringSubmatch(msg); len(m) == 2 {
		return fmt.Sprintf("Mỗi lần tối đa %s ảnh.", m[1]), true
	}
	return "", false
}

func matchPrefix(msg string) (string, bool) {
	low := strings.ToLower(msg)
	for _, p := range prefixes {
		prefix := strings.ToLower(p.prefix)
		if low == prefix || strings.HasPrefix(low, prefix+":") || strings.HasPrefix(low, prefix+" ") {
			return p.vi, true
		}
	}
	return "", false
}

func containsVietnamese(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

func looksLikeEnglish(s string) bool {
	letters := 0
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			letters++
		}
	}
	return letters >= 3
}
