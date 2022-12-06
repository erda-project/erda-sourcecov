package core

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/martian/log"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1opt "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/retry"

	"github.com/erda-project/erda-sourcecov/agent/conf"
)

var restClient *restclient.Config
var clientSet *kubernetes.Clientset

type Service struct {
	Name               string
	Image              string
	JarAddrList        []string
	Pods               []Pod
	LoadJarPackageLock sync.Mutex
	ErrorMessage       string

	IsDelete   bool
	ctx        context.Context
	cancelFunc func()
}

type Pod struct {
	Addr          string
	PodName       string
	ContainerName string
	ErrorMsg      string
	HasError      bool
}

var Services = sync.Map{}

func SetService(svcName string, svc *Service) {
	Services.Store(svcName, svc)
}

func GetService(svcName string) (*Service, bool) {
	value, ok := Services.Load(svcName)
	if !ok {
		return &Service{}, false
	}

	return value.(*Service), true
}

func DeleteService(svcName string) {
	RunJobs.Delete(svcName)
}

func setK8sClientSet() error {

	//var kubeconfig *string
	//if home := homedir.HomeDir(); home != "" {
	//	kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	//} else {
	//	kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	//}
	//flag.Parse()
	//
	//config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	//if err != nil {
	//	return err
	//}
	//restClient = config

	config, err := restclient.InClusterConfig()
	if err != nil {
		return err
	}
	restClient = config

	set, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	clientSet = set
	return nil
}

var WhenStartLoadAllDeploymentLock sync.Mutex
var deployListNum int
var watchListDeployLoadDoneNum = 0

func WatchJacocoPod(ctx context.Context) {
	WhenStartLoadAllDeploymentLock.Lock()

	go func() {

		err := setK8sClientSet()
		if err != nil {
			WhenStartLoadAllDeploymentLock.Unlock()
			log.Errorf("can not client k8s client")
			return
		}

		list, err := clientSet.AppsV1().Deployments(conf.Cfg.ProjectNs).List(ctx, v1opt.ListOptions{})
		if err != nil {
			WhenStartLoadAllDeploymentLock.Unlock()
			log.Errorf("get ns deploy list error %v", err)
			return
		}

		// preload all services at initialization
		for i := range list.Items {
			newServices := getServiceByDeploy(ctx, &list.Items[i])
			if newServices != nil {
				log.Infof("preload service %v", *newServices)
			}
			saveServices(newServices, true)
		}
		WhenStartLoadAllDeploymentLock.Unlock()

		deployListNum = len(list.Items)

		watchlist := cache.NewListWatchFromClient(
			clientSet.AppsV1().RESTClient(),
			"deployments", conf.Cfg.ProjectNs,
			fields.Everything())

		_, controller := cache.NewInformer(
			watchlist,
			&v1.Deployment{},
			0, //Duration is int64
			cache.ResourceEventHandlerFuncs{
				AddFunc: func(obj interface{}) {
					deploy, ok := obj.(*v1.Deployment)
					if ok {
						newServices := getServiceByDeploy(ctx, deploy)
						if watchListDeployLoadDoneNum < deployListNum {
							saveServices(newServices, true)
						} else {
							saveServices(newServices, false)
						}

						watchListDeployLoadDoneNum++
					} else {
						watchListDeployLoadDoneNum++
						log.Errorf("not a v1.Deployment type")
						jsn, _ := json.Marshal(obj)
						log.Infof("Deployment added: %s\n", jsn)
					}
				},
				DeleteFunc: func(obj interface{}) {
					deploy, ok := obj.(*v1.Deployment)
					if ok {
						deleteService(deploy.Name)
					} else {
						log.Errorf("not a v1.Deployment type")
						jsn, _ := json.Marshal(obj)
						log.Infof("Deployment added: %s\n", jsn)
					}
				},
				UpdateFunc: func(oldObj, newObj interface{}) {
					deploy, ok := newObj.(*v1.Deployment)
					if ok {
						newServices := getServiceByDeploy(ctx, deploy)
						saveServices(newServices, false)
					} else {
						log.Errorf("not a v1.Deployment type")
						jsn, _ := json.Marshal(newObj)
						log.Infof("Deployment added: %s\n", jsn)
					}
				},
			},
		)

		stop := make(chan struct{})
		defer close(stop)
		go controller.Run(stop)

		select {
		case <-ctx.Done():
			return
		}
	}()
}

func getServiceByDeploy(ctx context.Context, deploy *v1.Deployment) *Service {
	if deploy == nil {
		return nil
	}

	var newServices = Service{Name: deploy.Labels["app"]}
	for _, container := range deploy.Spec.Template.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "OPEN_JACOCO_AGENT" && env.Value == "true" {
				newServices.Image = container.Image
				break
			}
			if env.Name == "SOURCECOV_ENABLED" && env.Value == "true" {
				newServices.Image = container.Image
				break
			}
		}
	}

	if newServices.Image == "" {
		return nil
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		k8sPods, err := clientSet.CoreV1().Pods(conf.Cfg.ProjectNs).List(ctx, v1opt.ListOptions{
			LabelSelector: "app=" + newServices.Name,
		})
		if err != nil {
			return err
		}

		var pods []Pod
		for _, pod := range k8sPods.Items {
			pods = append(pods, Pod{
				Addr:          pod.Status.PodIP,
				PodName:       pod.Name,
				ContainerName: pod.Spec.Containers[0].Name,
			})
		}
		newServices.Pods = pods
		return nil
	})
	if err != nil {
		log.Errorf("can not get deployment pods, err %v", err)
		return nil
	}

	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	newServices.ctx = cancelCtx
	newServices.cancelFunc = cancelFunc
	newServices.LoadJarPackageLock = sync.Mutex{}

	return &newServices
}

func saveServices(svc *Service, sync bool) {
	if svc == nil {
		return
	}

	oldSvc, ok := GetService(svc.Name)
	if !ok {
		SetService(svc.Name, svc)

		go func() {
			select {
			case <-svc.ctx.Done():
				time.Sleep(10 * time.Minute)
				svc, ok := GetService(svc.Name)
				if !ok {
					return
				}

				if svc.IsDelete {
					DeleteService(svc.Name)
				}
				return
			}
		}()

		if !sync {
			go func() {
				err := reloadJarAddr(svc)
				if err != nil {
					log.Errorf("svc %v load jar addr error %v", svc.Name, err)
				}
			}()
		} else {
			err := reloadJarAddr(svc)
			if err != nil {
				log.Errorf("svc %v load jar addr error %v", svc.Name, err)
			}
		}
	} else {
		oldSvc.Pods = svc.Pods
		SetService(svc.Name, oldSvc)
		if oldSvc.Image != svc.Image {
			oldSvc.Image = svc.Image
			SetService(svc.Name, oldSvc)
			go func() {
				err := reloadJarAddr(svc)
				if err != nil {
					log.Errorf("svc %v load jar addr error %v", svc.Name, err)
				}
			}()
		}
	}

	return
}

func reloadJarAddr(svc *Service) error {
	svc.LoadJarPackageLock.Lock()
	defer svc.LoadJarPackageLock.Unlock()

	jarList, err := getServiceJarPackage(svc)

	service, ok := GetService(svc.Name)
	if !ok {
		return nil
	}

	if err != nil {
		errorMessage := fmt.Errorf("get svc %v jar package error %v", svc.Name, err)
		log.Errorf(errorMessage.Error())

		service.ErrorMessage = errorMessage.Error()
		SetService(svc.Name, service)
		return err
	}

	service.JarAddrList = jarList
	SetService(svc.Name, service)
	return nil
}

func getServiceJarPackage(svc *Service) ([]string, error) {
	log.Infof("start get svc %v jar package", svc.Name)
	defer log.Infof("end get svc %v jar package", svc.Name)

	imageJarTempPath, err := GenSvcJarImageTempDir(svc.Name)
	if err != nil {
		return nil, err
	}

	err = copyFromPod(svc.Pods[0].PodName, "/app", imageJarTempPath+"/app")
	if err != nil {
		log.Errorf("get pod jar path %v error %v", "/app", err)
		return nil, err
	}
	var jarAddrList []string
	err = filepath.Walk(imageJarTempPath+"/app", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".jar") {
			jarAddrList = append(jarAddrList, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return jarAddrList, nil
}

func copyFromPod(podName string, srcPath string, destPath string) error {
	r := restClient
	c := clientSet

	reader, outStream := io.Pipe()
	req := c.CoreV1().RESTClient().Get().
		Resource("pods").
		Name(podName).
		Namespace(conf.Cfg.ProjectNs).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			// 将数据转换成数据流
			Command: []string{"tar", "cf", "-", srcPath},
			Stdin:   true,
			Stdout:  true,
			Stderr:  true,
			TTY:     false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(r, "POST", req.URL())
	if err != nil {
		return err
	}

	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer outStream.Close()
		wait.Done()
		err = exec.Stream(remotecommand.StreamOptions{
			Stdin:  os.Stdin,
			Stdout: outStream,
			Stderr: os.Stderr,
			Tty:    false,
		})
	}()
	wait.Wait()
	if err != nil {
		return err
	}

	prefix := getPrefix(srcPath)
	prefix = path.Clean(prefix)
	prefix = stripPathShortcuts(prefix)
	return unTarAll(reader, destPath, prefix)
}

func unTarAll(reader io.Reader, destDir, prefix string) error {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}

		if !strings.HasPrefix(header.Name, prefix) {
			return fmt.Errorf("tar contents corrupted")
		}

		mode := header.FileInfo().Mode()
		destFileName := filepath.Join(destDir, header.Name[len(prefix):])

		baseName := filepath.Dir(destFileName)
		if err := os.MkdirAll(baseName, 0755); err != nil {
			return err
		}
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(destFileName, 0755); err != nil {
				return err
			}
			continue
		}

		evaledPath, err := filepath.EvalSymlinks(baseName)
		if err != nil {
			return err
		}

		if mode&os.ModeSymlink != 0 {
			linkname := header.Linkname

			if !filepath.IsAbs(linkname) {
				_ = filepath.Join(evaledPath, linkname)
			}

			if err := os.Symlink(linkname, destFileName); err != nil {
				return err
			}
		} else {
			outFile, err := os.Create(destFileName)
			if err != nil {
				return err
			}
			defer outFile.Close()
			if _, err := io.Copy(outFile, tarReader); err != nil {
				return err
			}
			if err := outFile.Close(); err != nil {
				return err
			}
		}
	}

	return nil
}

func stripPathShortcuts(p string) string {

	newPath := path.Clean(p)
	trimmed := strings.TrimPrefix(newPath, "../")

	for trimmed != newPath {
		newPath = trimmed
		trimmed = strings.TrimPrefix(newPath, "../")
	}

	// trim leftover {".", ".."}
	if newPath == "." || newPath == ".." {
		newPath = ""
	}

	if len(newPath) > 0 && string(newPath[0]) == "/" {
		return newPath[1:]
	}

	return newPath
}

func getPrefix(file string) string {
	return strings.TrimLeft(file, "/")
}

func unTarImage(unTarDir string, rootTar string, layers []string) error {
	err := simpleRun("", "tar", "-xf", rootTar, "-C", unTarDir)
	if err != nil {
		return err
	}
	var errInfo error
	for _, layer := range layers {
		err = simpleRun("", "tar", "-zxf", fmt.Sprintf("%v/%v.tar.gz", unTarDir, layer), "-C", unTarDir)
		if err != nil {
			errInfo = err
		}
	}
	fmt.Println("tar zxf error", errInfo)
	return nil
}

func simpleRun(dir string, name string, arg ...string) error {
	fmt.Fprintf(os.Stdout, "Run: %s, %v\n", name, arg)
	cmd := exec.Command(name, arg...)
	if dir != "" {
		cmd.Path = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func deleteService(name string) {
	svc, ok := GetService(name)
	if ok {
		svc.IsDelete = true
		svc.cancelFunc()
		SetService(name, svc)
	}
}
