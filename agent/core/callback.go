package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/google/martian/log"

	"github.com/erda-project/erda-sourcecov/agent/conf"
	"github.com/erda-project/erda-sourcecov/agent/pkg/httpclient"
)

type CallbackEndRequest struct {
	ID        uint64
	Status    string
	Msg       string
	ReportXml string `json:"reportXmlUUID"`
}

func callbackEnd(planID uint64, msg string, status CodeCoverageExecStatus, reportXmlAddr string) error {
	log.Infof("callbackEnd planID %v status %v \n", planID, status)

	var req = CallbackEndRequest{
		ID:     planID,
		Status: string(status),
		Msg:    msg,
	}
	if reportXmlAddr != "" {
		file, err := os.Open(reportXmlAddr)
		if err != nil {
			return fmt.Errorf("upload xml error %v", err)
		}
		fileData, err := uploadFile(conf.Cfg.CenterHost, conf.Cfg.CenterToken, planID, conf.Cfg.ProjectID, file)
		//fileData, err := uploadFile("https://erda.dev.terminus.io", conf.Cfg.CenterToken, planID, conf.Cfg.ProjectID, file)
		if err != nil {
			return fmt.Errorf("upload xml error %v", err)
		}
		req.ReportXml = fileData.UUID
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("json marshal req error %v", err)
	}
	reqBodyReader := bytes.NewReader(reqBody)

	request, err := http.NewRequest("POST", fmt.Sprintf("%v/api/code-coverage/actions/end-callBack", conf.Cfg.CenterHost), reqBodyReader)
	if err != nil {
		return fmt.Errorf("new request error %v", err)
	}
	defer func() {
		if request != nil && request.Body != nil {
			request.Body.Close()
		}
	}()
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	request.Header.Set("Authorization", conf.Cfg.CenterToken)
	request.Header.Set("Org", conf.Cfg.OrgName)
	request.Header.Set("USER-ID", "2")

	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("do client error %v", err)
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body error %v", err)
	}
	str := string(respBytes)
	if resp.StatusCode != 200 {
		return fmt.Errorf("response status code not 200, body: %v", str)
	}
	log.Infof("response body: %v", str)
	return nil
}

type callbackReportRequest struct {
	ID        uint64
	Status    string
	Msg       string
	ReportTar string `json:"reportTarUrl"`
}

func callbackReport(planID uint64, status CodeCoverageExecStatus, msg string, reportAddr string) error {
	log.Infof("callbackReport planID %v status %v \n", planID, status)

	var req = callbackReportRequest{
		ID:     planID,
		Status: string(status),
		Msg:    msg,
	}
	if reportAddr != "" {
		file, err := os.Open(reportAddr)
		if err != nil {
			return fmt.Errorf("upload html error %v", err)
		}

		fileData, err := uploadFile(conf.Cfg.CenterHost, conf.Cfg.CenterToken, planID, conf.Cfg.ProjectID, file)
		//fileData, err := uploadFile("https://erda.dev.terminus.io", conf.Cfg.CenterToken, planID, conf.Cfg.ProjectID, file)
		if err != nil {
			return fmt.Errorf("upload html error %v", err)
		}

		req.ReportTar = fileData.DownloadURL
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("json marshal req error %v", err)
	}
	reqBodyReader := bytes.NewReader(reqBody)

	request, err := http.NewRequest("POST", fmt.Sprintf("%v/api/code-coverage/actions/report-callBack", conf.Cfg.CenterHost), reqBodyReader)
	if err != nil {
		return fmt.Errorf("new request error %v", err)
	}
	defer func() {
		if request != nil && request.Body != nil {
			request.Body.Close()
		}
	}()
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	request.Header.Set("Authorization", conf.Cfg.CenterToken)
	request.Header.Set("Org", conf.Cfg.OrgName)
	request.Header.Set("USER-ID", "2")

	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("do client error %v", err)
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body error %v", err)
	}
	str := string(respBytes)
	if resp.StatusCode != 200 {
		return fmt.Errorf("response status code not 200, body: %v", str)
	}
	log.Infof("response body: %v", str)
	return nil
}

type callbackRequest struct {
	ID     uint64 `json:"id"`
	Status string
	Msg    string
}

func callbackReady(planID uint64, msg string) error {
	log.Infof("callbackReady planID %v status %v \n", planID, "ready")

	job, ok := GetJob(planID)
	if !ok {
		return nil
	}
	if job.Status == ReadyStatus {
		return nil
	}

	var req = callbackRequest{
		ID:     planID,
		Status: string(ReadyStatus),
		Msg:    msg,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("json marshal req error %v", err)
	}
	reqBodyReader := bytes.NewReader(reqBody)

	request, err := http.NewRequest("POST", fmt.Sprintf("%v/api/code-coverage/actions/ready-callBack", conf.Cfg.CenterHost), reqBodyReader)
	if err != nil {
		return fmt.Errorf("new request error %v", err)
	}
	defer func() {
		if request != nil && request.Body != nil {
			request.Body.Close()
		}
	}()
	request.Header.Set("Content-Type", "application/json;charset=UTF-8")
	request.Header.Set("Authorization", conf.Cfg.CenterToken)
	request.Header.Set("Org", conf.Cfg.OrgName)
	request.Header.Set("USER-ID", "2")

	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("do client error %v", err)
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body error %v", err)
	}
	str := string(respBytes)
	if resp.StatusCode != 200 {
		return fmt.Errorf("response status code not 200, body: %v", str)
	}
	log.Infof("response body: %v", str)
	return nil
}

type ErrorResponse struct {
	Code string      `json:"code"`
	Msg  string      `json:"msg"`
	Ctx  interface{} `json:"ctx"`
}

type Header struct {
	Success bool          `json:"success" `
	Error   ErrorResponse `json:"err"`
}

type CodeCoverageExecRecordDetailResp struct {
	Header
	Data *CodeCoverageExecRecordDetail `json:"data"`
}

type CodeCoverageExecRecordDetail struct {
	PlanID       uint64                 `json:"planID"`
	ProjectID    uint64                 `json:"projectID"`
	Status       CodeCoverageExecStatus `json:"status"`
	MavenSetting string                 `json:"mavenSetting"`
	Includes     string                 `json:"includes"`
	Excludes     string                 `json:"excludes"`
}

func status() (*CodeCoverageExecRecordDetail, error) {
	log.Infof("get projectID %v cover status", conf.Cfg.ProjectID)

	request, err := http.NewRequest("GET", fmt.Sprintf("%v/api/code-coverage/actions/status?projectID=%v&workspace=%v", conf.Cfg.CenterHost, conf.Cfg.ProjectID, conf.Cfg.Workspace), nil)
	if err != nil {
		return nil, fmt.Errorf("new request error %v", err)
	}
	defer func() {
		if request != nil && request.Body != nil {
			request.Body.Close()
		}
	}()
	request.Header.Set("Authorization", conf.Cfg.CenterToken)
	request.Header.Set("Org", conf.Cfg.OrgName)
	request.Header.Set("USER-ID", "2")

	client := http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("do client error %v", err)
	}
	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body error %v", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("response status code not 200, body: %v", string(respBytes))
	}

	log.Infof("response body: %v", string(respBytes))

	var detail CodeCoverageExecRecordDetailResp
	err = json.Unmarshal(respBytes, &detail)
	if err != nil {
		return nil, err
	}
	if !detail.Success {
		return nil, fmt.Errorf("response not success")
	}
	return detail.Data, nil
}

// FileUploadResponse 文件上传响应
type FileUploadResponse struct {
	Header
	Data *File `json:"data"`
}

type File struct {
	ID          uint64     `json:"id"`
	UUID        string     `json:"uuid"`
	DisplayName string     `json:"name"`
	ByteSize    int64      `json:"size"`
	DownloadURL string     `json:"url"`
	Type        string     `json:"type"`
	From        string     `json:"from"`
	Creator     string     `json:"creator"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ExpiredAt   *time.Time `json:"expiredAt,omitempty"`
}

func uploadFile(erdaAddr string, token string, planID uint64, projectID uint64, file *os.File) (*File, error) {
	var uploadResp FileUploadResponse

	multiparts := map[string]httpclient.MultipartItem{
		"file": {
			Reader:   file,
			Filename: file.Name(),
		},
	}

	resp, err := httpclient.New(httpclient.WithCompleteRedirect(), httpclient.WithTimeout(3*time.Minute, 3*time.Minute)).
		Post(erdaAddr).
		Path("/api/files").
		Param("fileFrom", fmt.Sprintf("jacoco-upload-%d-%d", planID, projectID)).
		Param("expiredIn", "168h").
		Header("Authorization", token).
		MultipartFormDataBody(multiparts).
		Do().JSON(&uploadResp)
	if err != nil {
		return nil, err
	}
	if !resp.IsOK() || !uploadResp.Success {
		return nil, fmt.Errorf("statusCode: %d, respError: %s", resp.StatusCode(), uploadResp.Error)
	}

	return uploadResp.Data, nil
}
