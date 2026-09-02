package services

// About holds the program/author credits shown in the "정보" dialog.
// Edit the constants below to change what is displayed. Empty fields are hidden.
type About struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	Author       string `json:"author"`
	Department   string `json:"department"`
	Organization string `json:"organization"`
	Year         string `json:"year"`
	Contact      string `json:"contact"`
	License      string `json:"license"`
}

// --- edit these for your credits -------------------------------------------
const (
	aboutAuthor       = "Dasan5"
	aboutDepartment   = "토목공학과 (Dept. of Civil Engineering)"
	aboutOrganization = "경상국립대학교 (Gyeongsang National University)"
	aboutYear         = "2026"
	aboutContact      = "kwonm@gnu.ac.kr"
	aboutLicense      = "사내 사용"
)

// ---------------------------------------------------------------------------

// AppInfo returns the program credits for the UI.
func (s *SettingsService) AppInfo() About {
	return About{
		Name:         "PaperKo",
		Version:      Version,
		Description:  "학술 논문 PDF 한국어 번역",
		Author:       aboutAuthor,
		Department:   aboutDepartment,
		Organization: aboutOrganization,
		Year:         aboutYear,
		Contact:      aboutContact,
		License:      aboutLicense,
	}
}
