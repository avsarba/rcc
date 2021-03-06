package operations

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"runtime"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/xviper"
)

const (
	issueHost = `https://telemetry.robocorp.com`
	issueUrl  = `/diagnostics-v1/issue`
)

func loadToken(reportFile string) (Token, error) {
	content, err := ioutil.ReadFile(reportFile)
	if err != nil {
		return nil, err
	}
	token := make(Token)
	err = token.FromJson(content)
	if err != nil {
		return nil, err
	}
	return token, nil
}

func createIssueZip(attachmentsFiles []string) (string, error) {
	zipfile := filepath.Join(conda.RobocorpTemp(), "attachments.zip")
	zipper, err := newZipper(zipfile)
	if err != nil {
		return "", err
	}
	defer zipper.Close()
	for index, attachment := range attachmentsFiles {
		niceName := fmt.Sprintf("%x_%s", index+1, filepath.Base(attachment))
		zipper.Add(attachment, niceName, nil)
	}
	return zipfile, nil
}

func createDiagnosticsReport() (string, error) {
	file := filepath.Join(conda.RobocorpTemp(), "diagnostics.txt")
	err := PrintDiagnostics(file, false)
	if err != nil {
		return "", err
	}
	return file, nil
}

func virtualName(filename string) (string, error) {
	digest, err := pathlib.Sha256(filename)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("attachments_%s.zip", digest[:16]), nil
}

func ReportIssue(reportFile string, attachmentsFiles []string) error {
	cloud.BackgroundMetric(common.ControllerIdentity(), "rcc.submit.issue", common.Version)
	token, err := loadToken(reportFile)
	if err != nil {
		return err
	}
	diagnostics, err := createDiagnosticsReport()
	if err == nil {
		attachmentsFiles = append(attachmentsFiles, diagnostics)
	}
	attachmentsFiles = append(attachmentsFiles, reportFile)
	filename, err := createIssueZip(attachmentsFiles)
	if err != nil {
		return err
	}
	shortname, err := virtualName(filename)
	if err != nil {
		return err
	}
	installationId := xviper.TrackingIdentity()
	token["installationId"] = installationId
	token["fileName"] = shortname
	token["controller"] = common.ControllerIdentity()
	_, ok := token["platform"]
	if !ok {
		token["platform"] = fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)
	}
	issueReport, err := token.AsJson()
	if err != nil {
		return err
	}
	common.Trace(issueReport)
	client, err := cloud.NewClient(issueHost)
	if err != nil {
		return err
	}
	request := client.NewRequest(issueUrl)
	request.Headers[contentType] = applicationJson
	request.Body = bytes.NewBuffer([]byte(issueReport))
	response := client.Post(request)
	json := make(Token)
	err = json.FromJson(response.Body)
	if err != nil {
		return err
	}
	postInfo, ok := json["attachmentPostInfo"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("Could not get attachmentPostInfo!")
	}
	url, ok := postInfo["url"].(string)
	if !ok {
		return fmt.Errorf("Could not get URL from attachmentPostInfo!")
	}
	fields, ok := postInfo["fields"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("Could not get fields from attachmentPostInfo!")
	}
	return MultipartUpload(url, toStringMap(fields), shortname, filename)
}

func toStringMap(entries map[string]interface{}) map[string]string {
	result := make(map[string]string)
	for key, value := range entries {
		text, ok := value.(string)
		if ok {
			result[key] = text
		}
	}
	return result
}
