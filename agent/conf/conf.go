package conf

type Conf struct {
	CenterHost  string `env:"CENTER_HOST" required:"true"`
	CenterToken string `env:"CENTER_TOKEN" required:"true"`

	ProjectID uint64 `env:"PROJECT_ID" required:"true"`
	ProjectNs string `env:"PROJECT_NS" required:"true"`
	OrgName   string `env:"ORG_NAME" required:"true"`
	Workspace string `env:"WORKSPACE" required:"true"`
}

const WorkDir = "/jacoco/work"
const JacocoCliAddr = "/app/jacococli.jar"
const ExtractCliAddr = "/app/extract-jar.sh"

var Cfg Conf

func init() {
	MustLoad(&Cfg)
}
