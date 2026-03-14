package picstore

type Config struct {
	PicPath string `env:"PIC_PATH" envDefault:"./tmp"`
}
