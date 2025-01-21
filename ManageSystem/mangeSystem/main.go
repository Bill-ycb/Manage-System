package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
)

// 学生结构体
type student struct {
	Name   string         `json:"name" form:"name"`
	Age    string         `json:"age" form:"age"`
	Sex    string         `json:"sex" form:"sex"`
	Class  string         `json:"class" form:"class"`
	Number string         `json:"number" form:"number"`
	Scores map[string]int `json:"score" form:"score"`
}

// ParseError 记录读取CSV文件错误的结构体
type ParseError struct {
	Line int
	Msg  string
}

var (
	students    = make(map[string]*student) //相当于数据库，存储所有学生信息
	mu          sync.Mutex
	wg          sync.WaitGroup
	studentChan = make(chan student, 1000)    //存储读取文件时的数据
	errorChan   = make(chan ParseError, 1000) //存储读取文件时产生的错误
)

func main() {
	r := gin.Default()
	studentGroup := r.Group("/student")
	{
		studentGroup.POST("/addStudent", addStudent)         //添加学生基本信息
		studentGroup.POST("/addScore", addOrUpdateScore)     //添加成绩或者修改成绩
		studentGroup.DELETE("/deleteStudent", deleteStudent) //根据学号删除学生信息
		studentGroup.DELETE("/deleteScore", deleteScore)     //删除学生成绩
		studentGroup.PUT("/updateStudent", updateStudent)    //更新学生信息
		studentGroup.GET("/getStudent", getStudent)          //根据学号查询基本信息和所有成绩信息
		studentGroup.GET("/getScore", getScore)              //根据学号和课程名称查询特定课程的信息
	}
	CSVGroup := r.Group("/csv")
	{
		CSVGroup.POST("/postFile", postFile)     //上传CSV文件
		CSVGroup.POST("/parseStudent", parseCSV) //读取CSV文件
	}

	err := r.Run()
	if err != nil {
		return
	}
}

func parseCSV(c *gin.Context) {
	//读取目录下的文件
	dir, err := os.ReadDir("./postFile")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
	for _, file := range dir {
		fmt.Println(file.Name())
		parseFile("postFile/" + file.Name())
		//删除已读的文件，防止后续文件重名的问题
		err := os.Remove("postFile/" + file.Name())
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"msg":  "操作成功",
		"data": ""})
}

// 读取CSV文件，导入学生信息
func parseFile(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		errorChan <- ParseError{Line: -1, Msg: fmt.Sprintf("无法打开文件：%v", err)}
		return
	}
	defer file.Close()
	reader := csv.NewReader(file)
	//通过goroutine开启多线程
	wg.Add(1)
	go func() {
		defer wg.Done()
		for lineNumber := 2; ; lineNumber++ {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				errorChan <- ParseError{Line: lineNumber, Msg: fmt.Sprintf("第%v行读取数据有误", lineNumber)}
				continue
			}
			//封装结构体数据
			student, err := parseStudent(record)
			if err != nil {
				errorChan <- ParseError{Line: lineNumber, Msg: fmt.Sprintf("解析失败%v", err)}
				continue
			}
			studentChan <- student
		}
		close(studentChan)
	}()
	fmt.Println(studentChan)
	numWorkers := 10
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(i)
	}

	wg.Wait()
	close(errorChan)
	fmt.Println(students)
}

func worker(id int) {
	//从通道读取结构体数值并存储到map中，使用sync保证原子性
	defer wg.Done()
	for student := range studentChan {
		mu.Lock()
		students[student.Number] = &student
		mu.Unlock()
	}
}

func parseStudent(record []string) (student, error) {
	if len(record) != 6 {
		return student{}, fmt.Errorf("CSV文件格式错误")
	}
	number := strings.TrimSpace(record[4])
	if number == "" {
		return student{}, fmt.Errorf("学号不能为空")
	}
	//依次获取结构体字段相应值
	name := strings.TrimSpace(record[0])
	age := strings.TrimSpace(record[1])
	sex := strings.TrimSpace(record[2])
	class := strings.TrimSpace(record[3])
	//单独处理json格式字段
	jsonString := strings.Trim(record[5], `"`)
	var scores map[string]int
	err := json.Unmarshal([]byte(jsonString), &scores)
	if err != nil {
		return student{}, err
	}
	//封装
	student := student{
		Name:   name,
		Age:    age,
		Sex:    sex,
		Class:  class,
		Number: number,
		Scores: scores,
	}
	return student, nil
}

func postFile(c *gin.Context) {
	// 确保请求中有文件上传
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有读取到文件"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	defer func(file multipart.File) {
		err := file.Close()
		if err != nil {
			return
		}
	}(file)
	// 创建保存文件的目录
	uploadDir := "./postFile"
	if _, err := os.Stat(uploadDir); os.IsNotExist(err) {
		err := os.Mkdir(uploadDir, 0755)
		if err != nil {
			return
		}
	}
	// 构建保存文件的完整路径
	dst, err := os.Create(filepath.Join(uploadDir, header.Filename))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法构建文件"})
		return
	}
	defer func(dst *os.File) {
		err := dst.Close()
		if err != nil {
			return
		}
	}(dst)
	// 复制文件内容到服务器
	if _, err := io.Copy(dst, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "上传失败"})
		return
	}
	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"msg":  "上传成功",
		"data": ""})
}

func getScore(c *gin.Context) {
	//从请求头中获取参数
	number := c.Query("number")
	lessonName := c.Query("lessonName")
	if lessonName == "" || number == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "信息输入不完全",
		})
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := students[number].Scores[lessonName]; ok {
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusOK,
			"msg":  "操作成功",
			"data": students[number].Scores[lessonName],
		})
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "输入科目不存在",
		})
	}

}

func updateStudent(c *gin.Context) {
	//从请求头中获取参数
	var updateData student
	number := c.Query("number")
	if err := c.ShouldBind(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	//判断是否已存在
	studentPtr, exists := students[number]
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "学生不存在"})
		return
	}

	// 使用反射来更新结构体字段
	v1 := reflect.ValueOf(updateData)
	v2 := reflect.ValueOf(studentPtr).Elem() // 获取指针指向的结构体的反射值
	for i := 0; i < v1.NumField(); i++ {
		f1 := v1.Field(i)
		if f1.IsZero() {
			continue // 如果字段是零值，则不更新
		}
		f2 := v2.Field(i)
		// 检查字段是否可设置
		if f2.CanSet() {
			f2.Set(f1)
		}
	}
	//若更新学号，则删除原来的学号数据
	if updateData.Number != "" {
		students[updateData.Number] = studentPtr
		delete(students, number)
	}
	c.JSON(http.StatusOK, gin.H{
		"msg":  "操作成功",
		"data": " ",
	})
}

func deleteScore(c *gin.Context) {
	var scores []string //等待删除的成绩列表
	if err := c.ShouldBind(&scores); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	number := c.Query("number")
	if number == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未输入学号或学号为空"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if _, ok := students[number]; ok {
		for _, v := range scores {
			_, ok := students[number].Scores[v]
			if ok {
				delete(students[number].Scores, v)
				c.JSON(http.StatusOK, gin.H{
					"code": http.StatusOK,
					"msg":  "操作成功",
					"data": " ",
				})
			} else {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "该学生不存在此科目的成绩",
					"科目":    v,
				})
				return
			}
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "学生不存在"})
		return
	}

}

func deleteStudent(c *gin.Context) {
	//从请求头中获取参数
	number := c.Query("number")
	if number == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未输入学号或学号为空"})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	//存在则进行删除操作，不存在则返回错误
	if _, ok := students[number]; ok {
		delete(students, number)
		c.JSON(http.StatusOK, gin.H{
			"msg":  "操作成功",
			"data": " ",
		})
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该学生不存在"})
	}
	return
}

func addOrUpdateScore(c *gin.Context) {
	//从请求头中获取参数
	var scores map[string]int
	if err := c.ShouldBind(&scores); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	number := c.Query("number")
	if students[number] != nil {
		//原来没有这一科目成绩就新增科目及成绩
		if students[number].Scores == nil {
			students[number].Scores = scores
		} else {
			for k, v := range scores {
				students[number].Scores[k] = v
			}
		} //存在就更新
		c.JSON(http.StatusOK, gin.H{
			"msg":  "操作成功",
			"data": "",
		})
	} else {
		c.JSON(http.StatusBadRequest, gin.H{
			"response": "学生不存在 ",
		})
	}

}

func addStudent(c *gin.Context) {
	//从请求头中获取参数
	var stu student
	if err := c.ShouldBindJSON(&stu); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if stu.Number == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "学号不能为空",
		})
		return
	}
	students[stu.Number] = &stu
	c.JSON(http.StatusOK, gin.H{
		"msg":  "操作成功",
		"data": students[stu.Number],
	})
}

func getStudent(c *gin.Context) {
	number := c.Query("number")
	mu.Lock()
	defer mu.Unlock()
	student := students[number]
	if student == nil {
		c.JSON(http.StatusNotFound, gin.H{"message": "该学生不存在"})
		fmt.Println(students)
	} else {
		c.JSON(http.StatusOK, gin.H{
			"code":    http.StatusOK,
			"msg":     "操作成功",
			"student": student})
		fmt.Println(students)
	}
}
