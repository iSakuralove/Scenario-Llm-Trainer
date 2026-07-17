package httpapi

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"situational-teaching/backend/internal/auth"
	"situational-teaching/backend/internal/domain"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request, path string) {
	switch path {
	case "register":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Username string `json:"username"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if !decode(w, r, &req) {
			return
		}
		password, err := auth.ValidatePassword(req.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		user, err := s.store.CreateUser(req.Username, req.Email, auth.HashPassword(password))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.audit(r, user, "auth.register", "user", user.ID, nil)
		access, refresh, err := s.auth.IssuePair(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": user, "access_token": access, "refresh_token": refresh})
	case "login":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Identifier string `json:"identifier"`
			Password   string `json:"password"`
		}
		if !decode(w, r, &req) {
			return
		}
		user, ok := s.store.FindUserByIdentifier(req.Identifier)
		if !ok || !auth.CheckPassword(req.Password, user.PasswordHash) {
			if !s.allowFailedLogin(w, r, req.Identifier) {
				return
			}
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		if auth.IsLegacyPasswordHash(user.PasswordHash) {
			upgraded, upgradeErr := s.store.UpgradeUserPasswordHash(user.ID, auth.HashPassword(req.Password))
			if upgradeErr != nil {
				s.audit(r, user, "auth.password_rehash_failed", "user", user.ID, map[string]string{
					"reason": truncateText(upgradeErr.Error(), 120),
				})
			} else {
				user = upgraded
				s.audit(r, user, "auth.password_rehashed", "user", user.ID, nil)
			}
		}
		s.audit(r, user, "auth.login", "user", user.ID, nil)
		access, refresh, err := s.auth.IssuePair(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": user, "access_token": access, "refresh_token": refresh})
	case "password-reset":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Identifier  string `json:"identifier"`
			NewPassword string `json:"new_password"`
			Token       string `json:"token"`
		}
		if !decode(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Token) == "" && s.allowAnonPasswordReset {
			user, ok := s.store.FindUserByIdentifier(req.Identifier)
			if !ok {
				writeError(w, http.StatusNotFound, "账号不存在")
				return
			}
			updated, resetErr := s.resetUserPassword(r, user, req.NewPassword)
			if resetErr != nil {
				writeError(w, http.StatusBadRequest, resetErr.Error())
				return
			}
			writeOK(w, map[string]interface{}{"user": updated})
			return
		}
		if strings.TrimSpace(req.Token) == "" && strings.TrimSpace(req.Identifier) != "" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if strings.TrimSpace(req.Token) == "" {
			writeError(w, http.StatusBadRequest, "重置令牌不能为空")
			return
		}
		claims, err := s.auth.Validate(req.Token)
		if err != nil || claims.Type != "password_reset" {
			writeError(w, http.StatusBadRequest, "重置链接无效或已过期")
			return
		}
		user, ok := s.store.GetUser(claims.Subject)
		if !ok || user.TokenVersion != claims.TokenVersion {
			writeError(w, http.StatusBadRequest, "重置链接无效或已使用")
			return
		}
		updated, err := s.resetUserPassword(r, user, req.NewPassword)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": updated})
	case "password-reset/request":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &req) {
			return
		}
		if s.smtpHost == "" || s.smtpFrom == "" || s.appPublicURL == "" {
			writeError(w, http.StatusServiceUnavailable, "邮件服务未配置")
			return
		}
		user, ok := s.store.FindUserByIdentifier(req.Email)
		if !ok || !strings.Contains(user.Email, "@") {
			writeOK(w, map[string]bool{"accepted": true})
			return
		}
		resetToken, err := s.auth.SignWithVersion(user.ID, user.Role, "password_reset", 10*time.Minute, user.TokenVersion)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "生成重置链接失败")
			return
		}
		link := s.appPublicURL + "/reset-password?token=" + url.QueryEscape(resetToken)
		if err := s.sendPasswordResetMail(user.Email, link); err != nil {
			writeError(w, http.StatusServiceUnavailable, "重置邮件发送失败")
			return
		}
		writeOK(w, map[string]bool{"accepted": true})
	case "password-reset/verify":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			Token string `json:"token"`
		}
		if !decode(w, r, &req) {
			return
		}
		if strings.TrimSpace(req.Token) == "" {
			writeError(w, http.StatusBadRequest, "重置令牌不能为空")
			return
		}
		claims, err := s.auth.Validate(req.Token)
		if err != nil || claims.Type != "password_reset" {
			writeError(w, http.StatusBadRequest, "重置链接无效或已过期")
			return
		}
		user, ok := s.store.GetUser(claims.Subject)
		if !ok || user.TokenVersion != claims.TokenVersion {
			writeError(w, http.StatusBadRequest, "重置链接无效或已使用")
			return
		}
		writeOK(w, map[string]bool{"valid": true})
	case "refresh":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			RefreshToken string `json:"refresh_token"`
		}
		if !decode(w, r, &req) {
			return
		}
		user, claims, err := s.authUser(req.RefreshToken)
		if err != nil || claims.Type != "refresh" {
			writeError(w, http.StatusUnauthorized, "invalid refresh token")
			return
		}
		access, refresh, err := s.auth.IssuePair(user)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": user, "access_token": access, "refresh_token": refresh})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}
func (s *Server) allowFailedLogin(w http.ResponseWriter, r *http.Request, identifier string) bool {
	if !s.allow(w, r, "login:fail:ip:"+clientIP(r), 10) {
		return false
	}
	identifierKey := strings.ToLower(strings.TrimSpace(identifier))
	if identifierKey == "" {
		return true
	}
	return s.allow(w, r, "login:fail:id:"+identifierKey, 5)
}
func (s *Server) resetUserPassword(r *http.Request, user *domain.User, newPassword string) (*domain.User, error) {
	password, err := auth.ValidatePassword(newPassword)
	if err != nil {
		return nil, err
	}
	updated, err := s.store.UpdateUserPassword(user.ID, auth.HashPassword(password))
	if err != nil {
		return nil, err
	}
	s.audit(r, updated, "auth.password_reset", "user", updated.ID, nil)
	return updated, nil
}

func (s *Server) sendPasswordResetMail(recipient, link string) error {
	port, err := strconv.Atoi(s.smtpPort)
	if err != nil || port <= 0 {
		return errors.New("invalid SMTP port")
	}
	message, err := buildPasswordResetMail(s.smtpFrom, recipient, link)
	if err != nil {
		return err
	}
	auth := smtp.PlainAuth("", s.smtpUsername, s.smtpPassword, s.smtpHost)
	return smtp.SendMail(
		s.smtpHost+":"+strconv.Itoa(port),
		auth,
		message.envelopeFrom,
		[]string{message.envelopeTo},
		message.data,
	)
}

type passwordResetMail struct {
	envelopeFrom string
	envelopeTo   string
	data         []byte
}

func buildPasswordResetMail(sender, recipient, link string) (passwordResetMail, error) {
	from, err := parseSMTPAddress(sender)
	if err != nil {
		return passwordResetMail{}, fmt.Errorf("invalid SMTP sender: %w", err)
	}
	to, err := parseSMTPAddress(recipient)
	if err != nil {
		return passwordResetMail{}, fmt.Errorf("invalid SMTP recipient: %w", err)
	}
	resetURL, err := url.Parse(link)
	if err != nil || (resetURL.Scheme != "http" && resetURL.Scheme != "https") || resetURL.Host == "" {
		return passwordResetMail{}, errors.New("invalid password reset URL")
	}

	var alternatives bytes.Buffer
	multipartWriter := multipart.NewWriter(&alternatives)
	plainText := "你正在重置情景式教学系统的登录密码。\r\n\r\n" +
		"请在 10 分钟内打开以下链接完成重置：\r\n" + link + "\r\n\r\n" +
		"如果这不是你的操作，请忽略此邮件。"
	htmlLink := html.EscapeString(link)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<body style="margin:0;padding:0;background:#f3f6f9;color:#17202a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif;">
  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="background:#f3f6f9;padding:32px 16px;">
    <tr>
      <td align="center">
        <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#ffffff;border:1px solid #dfe6ee;border-radius:18px;overflow:hidden;">
          <tr><td style="padding:28px 32px 14px;font-size:13px;font-weight:700;letter-spacing:.08em;color:#0f766e;">情景式教学系统</td></tr>
          <tr><td style="padding:0 32px 12px;font-size:26px;line-height:1.35;font-weight:800;color:#111827;">重置你的登录密码</td></tr>
          <tr><td style="padding:0 32px 24px;font-size:15px;line-height:1.8;color:#52606d;">此链接将在 10 分钟后失效，并且只能使用一次。</td></tr>
          <tr><td align="center" style="padding:0 32px 28px;"><a href="%s" style="display:inline-block;padding:13px 28px;border-radius:999px;background:#ff5a36;color:#ffffff;font-size:15px;font-weight:700;text-decoration:none;">重置密码</a></td></tr>
          <tr><td style="padding:0 32px 12px;font-size:13px;line-height:1.7;color:#697586;">按钮无法打开时，请复制下面的完整链接到浏览器：</td></tr>
          <tr><td style="padding:0 32px 26px;word-break:break-all;font-size:12px;line-height:1.7;"><a href="%s" style="color:#0f766e;text-decoration:underline;">%s</a></td></tr>
          <tr><td style="border-top:1px solid #e7edf3;padding:20px 32px 26px;font-size:12px;line-height:1.7;color:#7a8795;">如果这不是你的操作，请忽略此邮件，你的密码不会被修改。</td></tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`, htmlLink, htmlLink, htmlLink)

	if err := writeMailPart(multipartWriter, "text/plain; charset=UTF-8", plainText); err != nil {
		return passwordResetMail{}, err
	}
	if err := writeMailPart(multipartWriter, "text/html; charset=UTF-8", htmlBody); err != nil {
		return passwordResetMail{}, err
	}
	if err := multipartWriter.Close(); err != nil {
		return passwordResetMail{}, err
	}

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", from.String())
	fmt.Fprintf(&message, "To: %s\r\n", to.String())
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", "情景式教学系统密码重置"))
	fmt.Fprint(&message, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", multipartWriter.Boundary())
	message.Write(alternatives.Bytes())

	return passwordResetMail{
		envelopeFrom: from.Address,
		envelopeTo:   to.Address,
		data:         message.Bytes(),
	}, nil
}

func parseSMTPAddress(value string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("mail address contains a line break")
	}
	return mail.ParseAddress(strings.TrimSpace(value))
}

func writeMailPart(writer *multipart.Writer, contentType, content string) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Type", contentType)
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	encoded := quotedprintable.NewWriter(part)
	if _, err := encoded.Write([]byte(content)); err != nil {
		return err
	}
	return encoded.Close()
}
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, user *domain.User, suffix string) {
	switch suffix {
	case "":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, user)
	case "/profile":
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			TargetLevel      string   `json:"target_level"`
			TargetRole       *string  `json:"target_role"`
			PreferredDomains []string `json:"preferred_domains"`
			ResumeSummary    string   `json:"resume_summary"`
			ProjectSummary   string   `json:"project_summary"`
		}
		if !decode(w, r, &req) {
			return
		}
		profile := user.Profile
		if strings.TrimSpace(req.TargetLevel) != "" {
			profile.TargetLevel = strings.TrimSpace(req.TargetLevel)
		}
		if req.TargetRole != nil {
			profile.TargetRole = strings.TrimSpace(*req.TargetRole)
		}
		if req.PreferredDomains != nil {
			profile.PreferredDomains = req.PreferredDomains
		}
		profile.ResumeSummary = strings.TrimSpace(req.ResumeSummary)
		profile.ProjectSummary = strings.TrimSpace(req.ProjectSummary)
		updated, err := s.store.SaveUserProfile(user.ID, profile)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, updated)
	case "/profile/import":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		updated, err := s.importProfileResume(user, r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, updated)
	case "/password":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			NewPassword string `json:"new_password"`
		}
		if !decode(w, r, &req) {
			return
		}
		updated, err := s.resetUserPassword(r, user, req.NewPassword)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		access, refresh, err := s.auth.IssuePair(updated)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"user": updated, "access_token": access, "refresh_token": refresh})
	case "/dashboard":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		plan := s.learningPlan(user)
		calendar := reviewCalendarFromPlan(user, plan, time.Now())
		writeOK(w, map[string]interface{}{
			"user":             user,
			"stats":            user.Profile.TotalStats,
			"capability_radar": user.Profile.CapabilityRadar,
			"weak_points":      weakPointsFromPlan(plan, user.Profile.WeakPoints),
			"recommendations":  scenarioRecommendationsFromPlan(plan),
			"learning_plan":    plan,
			"review_calendar":  calendar,
		})
	case "/history":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, s.history(user.ID))
	case "/recommendations":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, s.learningPlan(user))
	case "/learning-plan":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, s.learningPlan(user))
	case "/review-calendar":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, reviewCalendarFromPlan(user, s.learningPlan(user), time.Now()))
	case "/mentor":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeOK(w, s.mentorSnapshot(user))
	case "/checkin":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		result, updated, err := s.checkin(user, time.Now())
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeOK(w, map[string]interface{}{"checkin": result, "user": updated})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}
func (s *Server) history(userID string) map[string]interface{} {
	scenarioSessions := s.store.ListScenarioSessionsForUser(userID)
	interviewSessions := s.store.ListInterviewSessionsForUser(userID)
	communityPosts := s.communityPostsForUserHistory(userID)
	sort.Slice(scenarioSessions, func(i, j int) bool {
		return scenarioSessions[i].StartedAt.After(scenarioSessions[j].StartedAt)
	})
	sort.Slice(interviewSessions, func(i, j int) bool {
		return interviewSessions[i].StartedAt.After(interviewSessions[j].StartedAt)
	})
	return map[string]interface{}{"scenarios": scenarioSessionViews(scenarioSessions), "interviews": interviewSessions, "community_posts": communityPosts}
}
