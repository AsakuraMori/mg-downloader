package comicDays

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

var COMIC_DAYS_INFO *ComicSession = nil

func GetFirstPageFromComicDays(url string) (string, string, error) {
	mgTitle, session, err := NewComicSession(url, "./cookies/cookie.cd.json")
	if err != nil {
		return "", "", fmt.Errorf("NewComicSession: %v", err)
	}
	COMIC_DAYS_INFO = session
	tempDir, err := os.MkdirTemp(os.TempDir(), "comicDays-*")
	session.Pages[0].Process(session.NetworkClient, session.Cookies, tempDir, 0)
	tmpDir := filepath.Join(tempDir, "000.png")
	data, err := os.ReadFile(tmpDir)
	if err != nil {
		return "", "", fmt.Errorf("ReadFile: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	encBase64 := "data:image/png;base64," + encoded
	defer os.Remove(tempDir)
	return mgTitle, encBase64, nil
}

func DownloadMangaFromComicDays(outDir string) error {
	for i, page := range COMIC_DAYS_INFO.Pages {
		pageNum := i + 1
		fmt.Printf("\nProcessing page %d of %d\n", pageNum, len(COMIC_DAYS_INFO.Pages))
		page.Process(COMIC_DAYS_INFO.NetworkClient, COMIC_DAYS_INFO.Cookies, outDir, pageNum)
	}
	return nil
}
