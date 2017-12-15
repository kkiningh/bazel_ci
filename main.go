package main

import (
  "flag"
  "log"
  "net/http"
  "os/exec"
  "path/filepath"
  "crypto/sha256"
  "encoding/hex"

  "github.com/gin-gonic/gin"
	"github.com/jinzhu/gorm"
  _ "github.com/jinzhu/gorm/dialects/sqlite"

  "gopkg.in/src-d/go-git.v4"
)

/***
 * Config
 ***/
type Config struct {
  BuildDir string
  DbDir    string
}

func initConfig(config *Config) {
  // Parse flags
  flag.StringVar(&config.BuildDir, "build_dir", "build", "Relative path to place builds")
  flag.StringVar(&config.DbDir, "database_dir", "db", "Relative path to place database")
  flag.Parse()
}

var config Config;

/***
 * Tasks
 ***/
const (
	Recieved string = "recieved"
	Running  string = "running"
	Finished string = "finished"
	Error    string = "error"
)

type Task struct {
  ID      uint   `json: id`
  Command string `json: command`
	Status  string `json: status`
	Stdout  string `json: stdout`
}

func MakeTask(db *gorm.DB, command string) Task {
	// Create a new task and save it to the database
  task := Task{
    Command: command,
    Status: Recieved,
  }
	db.Create(&task)

	return task
}

func TaskRun(db *gorm.DB, task Task) {
	// Mark the task as running
	task.Status = Running
	db.Save(task)

	// Run the command
	cmd := exec.Command(task.Command)
	if stdout, err := cmd.Output(); err != nil {
		TaskError(db, task)
		log.Panic(err)
	} else {
    // Mark the task as finished
    task.Stdout = string(stdout)
    task.Status = Finished
    db.Save(task)
  }
}

func TaskError(db *gorm.DB, task Task) {
	// Mark the task in error
	task.Status = Error
	db.Save(task)
}

/***
 * Endpoints
 ***/
func postNotify(c *gin.Context, db *gorm.DB) {
	// Create a new task
	task := MakeTask(db, "ls")

	// Start a thread to run the task
	go TaskRun(db, task)

	// Return the task id as a response
  c.JSON(http.StatusOK, gin.H{
    "id": task.ID,
	})
}

func getTaskStatus(c *gin.Context, db *gorm.DB) {
	var tasks []Task
	if err := db.Find(&tasks).Error; err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		log.Print(err)
		return
	}

	c.JSON(200, tasks)
}

func getTaskStatusById(c *gin.Context, db *gorm.DB, id string) {
  var task Task
  if err := db.Where("id = ?", id).First(&task).Error; err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		log.Print(err)
		return
	}

	c.JSON(200, task)
}

/***
* Hash
***/

func HashStrSHA256(str string) string {
  hasher := sha256.New()
  hasher.Write([]byte(str))
  return hex.EncodeToString(hasher.Sum(nil))
}

/***
 * Repos
 ***/
type Repository struct {
  Id         uint   `json:"id"`
  Directory  string `json:"directory"`
  Url        string `json:"url"`
  RemoteName string `json:"remoteName"`
}

func (repo *Repository) Clone() error {
  _, err := git.PlainClone(repo.Directory, false, &git.CloneOptions{
    URL: repo.Url,
    RemoteName: repo.RemoteName,
  })

  return err
}

func (repo *Repository) Pull() error {
  // Set the context to the given repo
  r, err := git.PlainOpen(repo.Directory)
  if err != nil {
    return err
  }

  // Query the worktree
  w, err := r.Worktree()
  if err != nil {
    return err
  }

  return w.Pull(&git.PullOptions{
    RemoteName: repo.RemoteName,
  })
}

func CreateRepository(db *gorm.DB, url string, remoteName string) (Repository, error) {
  repo := Repository{
    Directory:  filepath.Join(config.BuildDir, HashStrSHA256(url)),
    Url:        url,
    RemoteName: remoteName,
  }

  db.Create(&repo)

  if err := repo.Clone(); err != nil {
    return repo, err
  }

  return repo, nil
}

func GetRepositoryById(db *gorm.DB, id string) (Repository, error) {
  var repo Repository
  err := db.Where("id = ?", id).First(&repo).Error
  return repo, err
}

type CreateRepoRequest struct {
  Url string `json:"url" form:"url" binding:"required"`
}

func postRepoCreate(c *gin.Context, db *gorm.DB) {
  request := CreateRepoRequest{}
  if err := c.ShouldBindJSON(&request); err != nil {
    c.AbortWithStatus(http.StatusBadRequest)
    log.Printf("Failed to bind clone request: %s", err)
    return
  }

  repo, err := CreateRepository(db, request.Url, "origin")
  if err != nil {
    c.AbortWithStatus(http.StatusBadRequest)
    log.Printf("Failed to create repo: %s", err)
    return
  }

  c.JSON(http.StatusOK, repo)
}

func postRepoPull(c *gin.Context, db *gorm.DB, id string) {
  repo, err := GetRepositoryById(db, id)
  if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
    log.Printf("Database error: %s", err)
		return
	}

  if err := repo.Pull(); err != nil {
    if err == git.NoErrAlreadyUpToDate {
      log.Printf("Repository already up to date")
    } else {
      c.AbortWithStatus(http.StatusBadRequest)
      log.Printf("Failed to pull repo: %s", err)
      return
    }
  }

  c.JSON(http.StatusOK, repo)
}

func getRepoStatus(c *gin.Context, db *gorm.DB, id string) {
  repo, err := GetRepositoryById(db, id)
  if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
    log.Printf("Database error: %s", err)
		return
	}

  c.JSON(http.StatusOK, repo)
}

/***
 * Router
 ***/
func main() {
  initConfig(&config)

  // Create a new database connection
  dbFilepath := filepath.Join(config.DbDir, "sqlite3.db")
  db, err := gorm.Open("sqlite3", dbFilepath)
  if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

  // Auto migrate to the current model
  db.AutoMigrate(&Task{})
  db.AutoMigrate(&Repository{})

	// Create a new router
  r := gin.Default()
  //r.POST("/notify", func(c *gin.Context) {
  //  postNotify(c, db)
  //})
  r.POST("/repo/create", func(c *gin.Context) {
    postRepoCreate(c, db)
  })
  r.POST("/repo/pull/:id", func(c *gin.Context) {
    postRepoPull(c, db, c.Params.ByName("id"))
  })
  r.GET("/repo/status/:id", func(c *gin.Context) {
    getRepoStatus(c, db, c.Params.ByName("id"))
  })
  r.GET("/task/status", func(c *gin.Context) {
		getTaskStatus(c, db)
	})
  r.GET("/task/status/:id", func(c *gin.Context) {
    getTaskStatusById(c, db, c.Params.ByName("id"))
  })

  // listen and serve on localhost:8080
  r.Run()
}
