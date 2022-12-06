package core

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/martian/log"
	"k8s.io/client-go/util/retry"

	"github.com/erda-project/erda-sourcecov/agent/conf"
	"github.com/erda-project/erda-sourcecov/agent/pkg/limit_wait_group"
)

type DetectionJob struct {
	PlanID        uint64
	Status        CodeCoverageExecStatus
	JobStatus     string
	MavenSettings string
	Includes      string
	Excludes      string

	ErrorMsg string

	ctx        context.Context
	cancelFunc func()
	DumpLock   sync.Mutex
}

var RunJobs = sync.Map{}

func SetJob(planID uint64, job *DetectionJob) {
	RunJobs.Store(planID, job)
}

func GetJob(planID uint64) (*DetectionJob, bool) {
	value, ok := RunJobs.Load(planID)
	if !ok {
		return &DetectionJob{}, false
	}

	return value.(*DetectionJob), true
}

func DeleteJob(planID uint64) {
	RunJobs.Delete(planID)

}

func WatchJob(ctx context.Context) {
	WhenStartLoadAllDeploymentLock.Lock()
	WhenStartLoadAllDeploymentLock.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var detail *CodeCoverageExecRecordDetail
			var err error
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				detail, err = status()
				if err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				log.Errorf("query erda jacoco cover status error %v", err)
				time.Sleep(3 * time.Minute)
				continue
			}

			if detail == nil || detail.PlanID <= 0 {
				time.Sleep(3 * time.Minute)
				continue
			}
			saveJob(detail)
			time.Sleep(1 * time.Minute)
		}
	}
}

func schedulingJob(planID uint64) {

	go func() {
		for {
			job, ok := GetJob(planID)
			if !ok {
				return
			}

			select {
			case <-job.ctx.Done():
				time.Sleep(5 * time.Minute)
				err := simpleRun("", "rm", "-rf", GenPlanDumpExecDir(planID))
				if err != nil {
					log.Errorf("remove plan workdir error %v", err)
				}
				DeleteJob(planID)
				return
			default:
				if job.Status == FailStatus || job.Status == SuccessStatus || job.Status == CancelStatus {
					job.cancelFunc()
					return
				}

				if job.Status == EndingStatus {
					err := report(planID)
					if err != nil {
						callbackEndWithMessage(planID, fmt.Sprintf("report xml and html error %v", err))
					}
				}
				time.Sleep(30 * time.Second)
			}
		}
	}()

	log.Infof("start schedulingJob %v", planID)
	job, ok := GetJob(planID)
	if !ok {
		return
	}

	if job.JobStatus != Running {
		return
	}

	if !Exists(GenPlanDumpExecDir(planID)) {
		err := os.Mkdir(GenPlanDumpExecDir(planID), 0777)
		if err != nil {
			return
		}
	}

	err := loadClassSources(planID)
	if err != nil {
		callbackEndWithMessage(planID, fmt.Sprintf("loadClassSources error %v", err))
		return
	}

	dumpExec(planID)

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return callbackReady(planID, buildCallbackErrorMessage(planID))
	})
	if err != nil {
		callbackEndWithMessage(planID, fmt.Sprintf("callback ready error %v", err))
		return
	}

	job, ok = GetJob(planID)
	if !ok {
		return
	}
	job.JobStatus = Ready
	SetJob(planID, job)

	var loopTimes = 1
	for {
		select {
		case <-job.ctx.Done():
			return
		case <-time.After(5 * time.Minute):
			dumpExec(planID)
			loopTimes++
			if loopTimes > 10 {
				mergeAllSvcExec(planID)
				loopTimes = 1
			}
		}
	}
}

func report(planID uint64) error {
	job, ok := GetJob(planID)
	if !ok {
		return nil
	}
	if job.Status != EndingStatus {
		return nil
	}

	dumpExec(planID)
	svcExecMap := mergeAllSvcExec(planID)

	job.DumpLock.Lock()
	if len(svcExecMap) <= 0 {
		return fmt.Errorf("not find svc exec dump file")
	}
	projectExec, err := os.CreateTemp("", "_project_.exec")
	if err != nil {
		return fmt.Errorf("create project exec dump file error %v", err)
	}

	var svcExecList []string
	for _, v := range svcExecMap {
		svcExecList = append(svcExecList, v)
	}

	err = mergeExec(projectExec.Name(), svcExecList)
	if err != nil {
		return fmt.Errorf("merge all svc exec dump error %v", err)
	}
	job.DumpLock.Unlock()

	tempDir, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf("failed to create project xml temp file, error %v", err)
	}
	fileName := fmt.Sprintf("%v/%v", tempDir, "_project_xml")
	_, err = os.Create(fmt.Sprintf("%v/%v", tempDir, "_project_xml"))
	if err != nil {
		return fmt.Errorf("failed to create project xml temp file, error %v", err)
	}

	err = simpleRun("", "java", "-jar", conf.JacocoCliAddr, "report", projectExec.Name(), "--classfiles",
		GenProjectClassDir()+"/sub/libjarcls", "--sourcefiles", GenProjectClassDir()+"/sub/libjarsrc", "--xml", fileName)
	if err != nil {
		return fmt.Errorf("failed to report project xml cover, error %v", err)
	}

	// 压缩
	err = simpleRun("", "sh", "-c", fmt.Sprintf("cd %v && tar -czf %v %v", tempDir, "_project_xml.tar.gz", "_project_xml"))
	if err != nil {
		return fmt.Errorf("tar app report xml error %v", err)
	}

	var errorMessage = buildCallbackErrorMessage(planID)
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return callbackEnd(planID, errorMessage, SuccessStatus, fmt.Sprintf("%v/%v", tempDir, "_project_xml.tar.gz"))
	})
	if err != nil {
		return fmt.Errorf("report project cover xml error %v", err)
	}

	err = simpleRun("", "java", "-jar", conf.JacocoCliAddr, "report", projectExec.Name(), "--classfiles",
		GenProjectClassDir()+"/sub/libjarcls", "--sourcefiles", GenProjectClassDir()+"/sub/libjarsrc", "--html", fmt.Sprintf("%v/%v", tempDir, "_project_html"))
	if err != nil {
		return fmt.Errorf("faild report porject html, error %v", err)
	}

	var times = time.Now().Format("20060102150405")
	err = simpleRun("", "sh", "-c", fmt.Sprintf("cd %v && tar -czf %v %v", tempDir, times+".tar.gz", "_project_html"))
	if err != nil {
		return fmt.Errorf("failed tar project html dir, error %v", err)
	}

	err = callbackReport(planID, SuccessStatus, errorMessage, fmt.Sprintf("%v/%v", tempDir, times+".tar.gz"))
	if err != nil {
		return fmt.Errorf("failed report project html tar.gz, error %v", err)
	}

	fmt.Println("end report all")
	return nil
}

func callbackEndWithMessage(planID uint64, message string) {
	job, ok := GetJob(planID)
	if !ok {
		return
	}
	job.Status = FailStatus
	job.JobStatus = Fail
	job.cancelFunc()
	SetJob(planID, job)
	log.Errorf(message)
	err := callbackEnd(planID, message, FailStatus, "")
	if err != nil {
		log.Errorf("callback end error %v", err)
		return
	}
}

func buildCallbackErrorMessage(planID uint64) string {
	job, ok := GetJob(planID)
	if !ok {
		return ""
	}

	var msg = ""
	if job.ErrorMsg != "" {
		msg += job.ErrorMsg + "job error message: \n"
		msg += job.ErrorMsg + "\n"
	}

	var allMessage = ""
	Services.Range(func(key, value interface{}) bool {
		svc, ok := GetService(key.(string))
		if !ok {
			return true
		}

		var servicesMessage = ""
		if svc.ErrorMessage != "" {
			servicesMessage += svc.ErrorMessage + "\n"
		}

		var podErrorMessage = ""
		for _, pod := range svc.Pods {
			if pod.ErrorMsg != "" {
				podErrorMessage += pod.ErrorMsg + "\n"
			}
		}

		if servicesMessage != "" {
			servicesMessage = fmt.Sprintf("svc %v error message \n", svc.Name) + servicesMessage
		}
		if podErrorMessage != "" {
			podErrorMessage = fmt.Sprintf("svc %v pod error message \n", svc.Name) + podErrorMessage
		}

		allMessage = allMessage + servicesMessage + podErrorMessage
		return true
	})

	if allMessage != "" {
		msg += allMessage + "\n"
	}

	if len(msg) > 1500 {
		msg = msg[:1500]
	}

	return msg
}

func loadClassSources(planID uint64) error {
	var jarAddrList []string
	Services.Range(func(key, value interface{}) bool {
		svc, ok := GetService(key.(string))
		if !ok {
			return true
		}

		if svc.ErrorMessage != "" {
			return true
		}

		for _, jarAddr := range svc.JarAddrList {
			jarAddrList = append(jarAddrList, jarAddr)
		}
		return true
	})

	if len(jarAddrList) <= 0 {
		return fmt.Errorf("all service not find jar path")
	}

	jarAddrStr := strings.Join(jarAddrList, ",")
	err := simpleRun("", "/bin/bash", "-lc", fmt.Sprintf("bash %v %v %v", conf.ExtractCliAddr, jarAddrStr, GenProjectClassDir()))
	if err != nil {
		log.Errorf("failed to get all svc jar classes and sources, error %v", err)
		callbackError := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return callbackEnd(planID, fmt.Sprintf("failed to get all svc jar classes and sources, error %v", err), FailStatus, "")
		})
		if callbackError != nil {
			log.Errorf("error to callback %v, error %v", FailStatus, err)
			return callbackError
		}
		return err
	}

	return nil
}

func dumpExec(planID uint64) {
	job, ok := GetJob(planID)

	if !ok {
		return
	}
	job.DumpLock.Lock()
	defer job.DumpLock.Unlock()

	var wait = limit_wait_group.NewSemaphore(5)
	Services.Range(func(key, value interface{}) bool {
		select {
		case <-job.ctx.Done():
			return false
		default:
			wait.Add(1)
			go func(key string) {
				defer wait.Done()

				svc, ok := GetService(key)
				if !ok {
					return
				}
				if svc.IsDelete {
					return
				}
				var svcErrorMessage string

				if !Exists(GenSvcDumpExecDir(planID, svc.Name)) {
					err := simpleRun("", "mkdir", "-p", GenSvcDumpExecDir(planID, svc.Name))
					if err != nil {
						svcErrorMessage += fmt.Sprintf("create svc %v temp error %v", svc.Name, err)
						log.Errorf("create svc %v temp error %v", svc.Name, err)
						return
					}
				}

				svcExec, err := os.CreateTemp(GenSvcDumpExecDir(planID, svc.Name), "svc_"+svc.Name+"_*.exec")
				if err != nil {
					svcErrorMessage += fmt.Sprintf("create svc %v temp error %v", svc.Name, err)
					log.Errorf("create svc %v temp error %v", svc.Name, err)
					return
				}
				log.Infof("begin dump execinfo for pods: %v", svc.Pods)

				var podExecList []string
				var podErrorMap = map[string]string{}
				for podIndex, pod := range svc.Pods {

					if pod.HasError {
						continue
					}

					conn, err := net.DialTimeout("tcp", pod.Addr+":6300", 5*time.Second)
					if err != nil {
						podErrorMap[pod.Addr] = fmt.Sprintf("svc %v container %v fail to dial error %v", svc.Name, pod.Addr, err)
						continue
					}
					conn.Close()

					f, err := os.CreateTemp("", "svc_pod_"+strconv.FormatInt(int64(podIndex), 10))
					if err != nil {
						podErrorMap[pod.Addr] = fmt.Sprintf("svc %v container %v fail to create temp file error %v", svc.Name, pod.Addr, err)
						return
					}

					err = simpleRun("", "java", "-jar", conf.JacocoCliAddr, "dump", "--address", pod.Addr, "--destfile", f.Name(), "--port", "6300", "--reset")
					if err != nil {
						podErrorMap[pod.Addr] = fmt.Sprintf("svc %v container %v dump exec error %v", svc.Name, pod.Addr, err)
						return
					}
					podExecList = append(podExecList, f.Name())
				}

				err = mergeExec(svcExec.Name(), podExecList)
				if err != nil {
					svcErrorMessage += fmt.Sprintf("merge pod exec error %v", err)
				}

				nowSvc, ok := GetService(key)
				if !ok {
					return
				}
				if nowSvc.IsDelete {
					return
				}
				nowSvc.ErrorMessage += svcErrorMessage
				for podIndex := range nowSvc.Pods {
					if podErrorMap[nowSvc.Pods[podIndex].Addr] != "" {
						nowSvc.Pods[podIndex].ErrorMsg = podErrorMap[nowSvc.Pods[podIndex].Addr]
						nowSvc.Pods[podIndex].HasError = true
					}
				}
				SetService(svc.Name, nowSvc)
			}(key.(string))
		}
		return true
	})
	wait.Wait()
	return
}

func mergeAllSvcExec(planID uint64) map[string]string {
	job, ok := GetJob(planID)

	if !ok {
		return nil
	}

	job.DumpLock.Lock()
	defer job.DumpLock.Unlock()

	var mergeAllSvcExecList = map[string]string{}

	Services.Range(func(key, value interface{}) bool {
		select {
		case <-job.ctx.Done():
			return false
		default:
			svc, ok := GetService(key.(string))
			if !ok {
				return true
			}
			if svc.IsDelete {
				return true
			}

			files, err := ioutil.ReadDir(GenSvcDumpExecDir(planID, svc.Name))
			if err != nil {
				log.Errorf("merge all svc exec error %v", err)
				return true
			}

			var svcExecList []string
			for _, f := range files {
				if strings.HasSuffix(f.Name(), ".exec") {
					svcExecList = append(svcExecList, GenSvcDumpExecDir(planID, svc.Name)+"/"+f.Name())
				}
			}

			svcExec, err := os.CreateTemp(GenSvcDumpExecDir(planID, svc.Name), "svc_"+svc.Name+"_*.exec")
			if err != nil {
				log.Errorf("create svc %v error %v", svc.Name, err)
				return true
			}

			err = mergeExec(svcExec.Name(), svcExecList)
			if err != nil {
				nowSvc, ok := GetService(svc.Name)
				if !ok {
					return true
				}
				if nowSvc.IsDelete {
					return true
				}
				nowSvc.ErrorMessage += fmt.Sprintf("merge pod exec error %v", err)
				SetService(svc.Name, nowSvc)
			}

			mergeAllSvcExecList[svc.Name] = svcExec.Name()

			for _, f := range svcExecList {
				err := os.Remove(f)
				if err != nil {
					log.Errorf("remove svc %v file %v error %v", svc.Name, f, err)
				}
			}
		}
		return true
	})
	return mergeAllSvcExecList
}

func mergeExec(destFile string, files []string) error {
	var args []string
	args = append(args, "-jar")
	args = append(args, conf.JacocoCliAddr)
	args = append(args, "merge")
	for _, file := range files {
		args = append(args, file)
	}
	args = append(args, "--destfile")
	args = append(args, destFile)
	err := simpleRun("", "java", args...)
	if err != nil {
		return err
	}
	return nil
}

func saveJob(detail *CodeCoverageExecRecordDetail) {
	if detail == nil {
		return
	}

	job, ok := GetJob(detail.PlanID)

	if !ok || job.PlanID != detail.PlanID {
		var newJob = DetectionJob{
			PlanID:        detail.PlanID,
			Status:        detail.Status,
			MavenSettings: detail.MavenSetting,
			Includes:      detail.Includes,
			Excludes:      detail.Excludes,
		}

		ctx, cancel := context.WithCancel(context.Background())
		newJob.ctx = ctx
		newJob.cancelFunc = cancel
		if detail.Status == RunningStatus || detail.Status == Ready {
			newJob.JobStatus = Running
		}

		RunJobs.Range(func(key, value interface{}) bool {
			job := value.(*DetectionJob)
			job.cancelFunc()
			return true
		})

		if !Exists("/usr/share/maven/conf/settings.xml") {
			err := simpleRun("", "bash", "-c", "cd /usr/share/maven/conf && touch settings.xml")
			if err != nil {
				log.Errorf("error to create setting file %v", err)
				return
			}
		}
		f, err := os.OpenFile("/usr/share/maven/conf/settings.xml", os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			log.Errorf("error to write to setting file %v", err)
			return
		}
		defer f.Close()
		_, err = f.WriteString(detail.MavenSetting)
		if err != nil {
			return
		}

		os.Setenv("INCLUDES", detail.Includes)
		os.Setenv("EXCLUDES", detail.Excludes)

		SetJob(detail.PlanID, &newJob)
		go schedulingJob(detail.PlanID)
	} else {
		job.Status = detail.Status
		SetJob(detail.PlanID, job)
	}
}
