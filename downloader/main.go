package main

import (
	"database/sql"
	"errors"
	"fmt"
	"git.eycia.me/eycia/configparser"
	_ "github.com/go-sql-driver/mysql"
	log "github.com/Sirupsen/logrus"
	"github.com/zbindenren/logrus_mail"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

//fmttm tss

//------------------------------------------Download Machine-----------------------------------------
var (
	path = ""
)

func writeToDisk(rc io.ReadCloser, fn string) error {
	file, err := os.Create(fn)
	if err != nil {
		return err
	}
	defer rc.Close()
	defer file.Close()

	buf := make([]byte, 32*1024)

	_, err = io.CopyBuffer(file, rc, buf)
/*
	_, err = bufio.NewWriter(file).ReadFrom(rc)
*/
	if err != nil {
		return err
	}
	return nil
}

var (
	db     *sql.DB
	client *http.Client
)

func GetPictureReturnsReader(url string) (io.ReadCloser, string, error) {
	req, err := http.NewRequest("GET", url, nil)
	req.Header.Add("User-Agent", `Mozilla/5.0 (Linux; Android 4.3; Nexus 7 Build/JSS15Q) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/42.0.2307.2 Safari/537.36`)

	var resp *http.Response

	for try := 0; try < 5; try++ {
		resp, err = client.Do(req)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, "", err
	}

	content_type := resp.Header.Get("Content-Type")

	flag := false

	extName := ""

	for i, typ := range config.TypeAcceptable {
		//why use hasPrefix is because of some website returns something like "image/jpg; charset=UTF-8"
		//and that's not illegal
		if strings.HasPrefix(content_type, typ) {
			//if content_type == typ {
			flag = true
			extName = config.TypeExtName[i]
			break
		}
	}
	if !flag {
		resp.Body.Close()
		return nil, "", errors.New("image content-type not match, the content-type is" + content_type)
	}

	return resp.Body, extName, nil
}

func downloadAndStoreTask(uid int, nodeNum int) {
	log.WithField("thread id", uid).Info("Thread started")

	var URL string
	var id string

	//------------------------ process ----------------------
	for {
		result, err := updateStmt.Exec(uid)

		if err != nil {
			log.WithField("thread id", uid).Error("Failed to update REASON : ", err.Error())
			continue
		}

		if n, err := result.RowsAffected(); err != nil {
			log.WithField("thread id", uid).Error("Failed to get RowsAffected REASON :", err.Error())
			continue
		} else if n == 0 {
			//太烦 log.WithField("thread id", uid).Infof("No task Thread %d sleep %ds", uid, config.ThreadNoTaskSleepTime)
			time.Sleep(time.Second * (time.Duration)(config.ThreadNoTaskSleepTime))
			continue
		}

		rows, err := selectStmt.Query(uid)

		rowsUrl := make([]string, 0, config.NumberOfEachFetchTask)
		rowsId := make([]string, 0, config.NumberOfEachFetchTask)

		if err != nil {
			log.Error("Failed to get rows!!!! REASON : ", err.Error())
			rows.Close()
			continue
		}

		log.WithField("thread id", uid).Info("get rows successfully")

		p := 0
		for rows.Next() {
			if err := rows.Scan(&URL, &id); err != nil {
				log.WithField("thread id", uid).Error("Failed to scan url or/and id REASON : ", err.Error())
				rows.Close()
				continue
			}
			rowsUrl = append(rowsUrl, URL)
			rowsId = append(rowsId, id)
			p++
		}
		if err := rows.Err(); err != nil {
			log.WithField("thread id", uid).Error("rows returns error REASON : ", err.Error())
		}
		rows.Close()

		for i := 0; i < p; i++ {
			URL = rowsUrl[i]
			id = rowsId[i]

			log.WithFields(log.Fields{"thread id": uid, "picture id":id}).Info("Start to download ", URL)

			reader, extName, err := GetPictureReturnsReader(URL)
			if err != nil {
				log.WithFields(log.Fields{"thread id": uid, "picture id":id}).Errorf("Failed to download URL : [%s], REASON : [%s]", URL, err.Error())
				_, _ = errorStmt.Exec(id)
				continue
			}

			fn := path+id+"."+extName
			err = writeToDisk(reader, fn)

			if err != nil {
				log.WithFields(log.Fields{"thread id": uid, "picture id":id}).Errorf("Failed to write to disk PATH : [%s], REASON : [%s]", path+id+"."+extName, err.Error())
				_, _ = errorStmt.Exec(id)
				continue
			}

			_, err = finishStmt.Exec(nodeNum, id)

			if err != nil {
				log.WithFields(log.Fields{"thread id": uid, "picture id":id}).Errorf("Failed to finish task ID : [%s], REASON : [%s]", id, err.Error())
				os.Remove(fn)
				_, _ = errorStmt.Exec(id)
				continue
			}

			log.WithFields(log.Fields{"thread id": uid, "picture id":id}).Info("Finished download ", URL)
		}
	}

	//almost there
}

type Config struct {
	RootPath string `default:"/data/pictures/img1/"`

	NodeID int `default:"0"`

	ThreadNumber          int `default:"5"`
	ThreadStartDelay      int `default:"1"`  //with second
	ThreadNoTaskSleepTime int `default:"20"` //with second
	NumberOfEachFetchTask int `default:"5"`

	QueueTableName string `default:"pic_task_queue"`

	DBAddress  string `default:"fake.com"`
	DBPort     string `default:"3306"`
	DBName     string `default:"msghub"`
	DBUsername string `default:"root"`
	DBPassword string `default:"123456"`

	TypeAcceptable []string `default:"[\"image/png\", \"image/jpeg\", \"image/gif\"]"`
	TypeExtName    []string `default:"[\"png\", \"jpg\", \"gif\"]"`

	ConnectTimeout int `default:"10"` //with second

	MailEnable bool `default:"false"`

	MailApplicationName string `default:"IMGDownloader"`
	MailSMTPAddress     string `default:"127.0.0.1"`
	MailSMTPPort        int    `default:"25"`
	MailFrom            string `default:"fake@fake.com"`
	MailTo              string `default:"recv@fake.com"`

	MailUsername string `default:"nomailusername"`
	MailPassword string `default:"nomailpassword"`
}

var config Config

func loadConfig() {
	var err error

	//load
	configparser.AutoLoadConfig("downloader", &config)

	if config.RootPath[len(config.RootPath)-1] != '/' {
		config.RootPath += "/"
	}

	path = config.RootPath

	err = os.MkdirAll(path, 0777)
	if err != nil {
		panic(err)
	}
	log.Info("Load config end")
}

var (
	updateStmt *sql.Stmt
	selectStmt *sql.Stmt
	finishStmt *sql.Stmt
	errorStmt  *sql.Stmt
)

func main() {
	loadConfig()

	//progress connect client
	client = &http.Client{
		Timeout: time.Duration(time.Second * (time.Duration)(config.ConnectTimeout)),
	}

	//process log's mail sending
	if config.MailEnable {
		mailhook_auth, err := logrus_mail.NewMailAuthHook(config.MailApplicationName, config.MailSMTPAddress, config.MailSMTPPort, config.MailFrom, config.MailTo,
			config.MailUsername, config.MailPassword)

		if err == nil {
			log.AddHook(mailhook_auth)
			log.Error("Don't Worry, just for send a email to test")
		} else {
			log.Error("Can't Hook mail, ERROR:", err.Error())
		}
	}

	//generate sql session
	//root:123456@tcp(db.dianm.in:3306)/pictures
	url := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?timeout=20s", config.DBUsername, config.DBPassword, config.DBAddress, config.DBPort, config.DBName)
	db, err := sql.Open("mysql", url)
	if err != nil {
		log.Error("Can't Connect DB REASON : " + err.Error())
		return
	}
	err = db.Ping()
	if err != nil {
		log.Error("Can't Connect DB REASON : " + err.Error())
		return
	}

	updateStmt, err = db.Prepare(fmt.Sprintf(`
	UPDATE %s
		SET status = 1, owner = ?, time = CURRENT_TIMESTAMP, trytimes = trytimes + 1
		WHERE status = 0 AND owner = 0
		LIMIT %d;
	`, config.QueueTableName, config.NumberOfEachFetchTask))

	if err != nil {
		log.Error("Failed to Prepare1 ", err.Error())
		return
	}

	selectStmt, err = db.Prepare(fmt.Sprintf(`
	SELECT url, id from %s
		WHERE status = 1 AND owner = ?;
	`, config.QueueTableName))

	if err != nil {
		log.Error("Failed to Prepare2 ", err.Error())
		return
	}

	finishStmt, err = db.Prepare(fmt.Sprintf(`
    UPDATE %s
        SET status = 2, nodenum = ?
        WHERE id = ?;
	`, config.QueueTableName))

	if err != nil {
		log.Error("Failed to Prepare3 ", err.Error())
		return
	}

	errorStmt, err = db.Prepare(fmt.Sprintf(`
    UPDATE %s
        SET status = 3
        WHERE id = ?;
	`, config.QueueTableName))

	if err != nil {
		log.Error("Failed to Prepare4 ", err.Error())
		return
	}

	//start download task
	for taskNo := 0; taskNo < config.ThreadNumber-1; taskNo++ {
		go downloadAndStoreTask(config.NodeID*100 + taskNo, config.NodeID)
		time.Sleep(time.Second * (time.Duration)(config.ThreadStartDelay))
	}
	if config.ThreadNumber > 0 {
		downloadAndStoreTask(config.NodeID*100 + config.ThreadNumber - 1, config.NodeID)
	}
}