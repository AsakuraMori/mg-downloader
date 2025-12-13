package ourfeel

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

//func main() {
//	fmt.Println("Comic Days Manga Downloader and Deobfuscator")
//	fmt.Println("============================================")
//
//	fmt.Println("\nStage 1: Initialization")
//	fmt.Println("- This stage prepares the environment and retrieves manga information.")
//
//	session, err := NewComicSession("https://ourfeel.jp/episode/2551460909837012072")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	fmt.Println("\nStage 2: Downloading and Deobfuscating Pages")
//	fmt.Println("- This stage downloads, deobfuscates, and saves each page of the manga.")
//	fmt.Printf("pages: %d\n", len(session.Pages))
//	for i, page := range session.Pages {
//		pageNum := i + 1
//		fmt.Printf("\nProcessing page %d of %d\n", pageNum, len(session.Pages))
//		page.Process(session.NetworkClient, session.OutDir, pageNum)
//	}
//
//	fmt.Println("\nStage 3: Completion")
//	fmt.Println("- All pages have been processed and saved.")
//	fmt.Printf("- You can find the downloaded manga in the directory: %s\n", session.OutDir)
//}

var OURFEEL_INFO *ComicSession

func GetFirstPageFromOurfeel(url string) (string, string, error) {
	mgTitle, session, err := NewComicSession(url)
	if err != nil {
		return "", "", fmt.Errorf("NewComicSession: %v", err)
	}
	OURFEEL_INFO = session
	tempDir, err := os.MkdirTemp(os.TempDir(), "ourfeel-*")
	session.Pages[0].Process(session.NetworkClient, tempDir, 0)
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

func DownloadMangaFromOurfeel(outDir string) error {
	for i, page := range OURFEEL_INFO.Pages {
		pageNum := i + 1
		fmt.Printf("\nProcessing page %d of %d\n", pageNum, len(OURFEEL_INFO.Pages))
		page.Process(OURFEEL_INFO.NetworkClient, outDir, pageNum)
	}
	return nil
}
