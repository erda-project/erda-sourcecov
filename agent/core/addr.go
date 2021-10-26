package core

import (
	"fmt"
	"os"

	"github.com/erda-project/erda-sourcecov/agent/conf"
)

func GenPlanDumpExecDir(planID uint64) string {
	return fmt.Sprintf("%v/%v", conf.WorkDir, planID)
}

func GenSvcDumpExecDir(planID uint64, svcName string) string {
	return fmt.Sprintf("%v/%v/%v", conf.WorkDir, planID, svcName)
}

func GenSvcJarDir(svcName string) string {
	return fmt.Sprintf("%v/service/%v", conf.WorkDir, svcName)
}

func GenSvcJarImageTempDir(svcName string) (string, error) {
	return os.MkdirTemp("", svcName+"svc-jar-*")
}

func GenSvcClassDir(svcName string) string {
	return fmt.Sprintf("%v/class/%v", conf.WorkDir, svcName)
}

func GenProjectClassDir() string {
	return fmt.Sprintf("%v/class/_project_", conf.WorkDir)
}
