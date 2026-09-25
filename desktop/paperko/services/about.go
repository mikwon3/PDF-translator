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
	aboutAuthor       = "Minho Kwon (@mikwon3)"
	aboutDepartment   = ""
	aboutOrganization = ""
	aboutYear         = "2026"
	aboutContact      = "mikwon@me.com"
	aboutLicense      = "AGPL-3.0"
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
