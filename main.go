package main

import (
  "bytes"
  "log"
  "os/exec"
  "github.com/gin-gonic/gin"
)

func main() {
  r := gin.Default()
  r.POST("/ping", func(c *gin.Context) {
    cmd := exec.Command("ls")

    var out bytes.Buffer
    cmd.Stdout = &out
    if err := cmd.Run(); err != nil {
        log.Fatal(err)
    }

    c.JSON(200, gin.H{
      "message": out.String(),
    })
  })
  r.Run() // listen and serve on 0.0.0.0:8080
}
