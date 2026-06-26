package main
import (
	"fmt"
	"os"
)
func main() {
	fmt.Println("RENDER_EXTERNAL_URL:", os.Getenv("RENDER_EXTERNAL_URL"))
}
