package main

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddStudent(t *testing.T) {
	r := gin.Default()
	r.POST("/student/addStudent", addStudent)
	ts := httptest.NewServer(r)
	defer ts.Close()

	testStudent := student{
		Name:   "John Doe",
		Age:    "20",
		Sex:    "Male",
		Class:  "Computer Science",
		Number: "123456",
		Scores: map[string]int{
			"math": 80,
		},
	}

	testStudentJSON, err := json.Marshal(testStudent)
	assert.NoError(t, err)

	resp, err := http.Post(ts.URL+"/student/addStudent", "application/json", bytes.NewBuffer(testStudentJSON))
	assert.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	assert.NoError(t, err)

	dataMap := result["data"].(map[string]interface{})

	scoresMap, ok := dataMap["score"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected scores to be a map[string]interface{}, but got %T", dataMap["score"])
	}

	expectedScores := map[string]interface{}{
		"math": 80,
	}

	// 遍历并比较映射中的每个键值对
	for key, expectedValue := range expectedScores {
		actualValue, ok := scoresMap[key].(int)
		if !ok {

			if floatValue, floatOk := scoresMap[key].(float64); floatOk {
				actualValue = int(floatValue) // 将 float64 转换为 int 进行比较
			} else {
				t.Fatalf("expected scores[%s] to be an int or float64, but got %T", key, scoresMap[key])
			}
		}
		if actualValue != expectedValue {
			t.Errorf("expected scores[%s] to be %d, but got %d", key, expectedValue, actualValue)
		}
	}
	assert.Equal(t, "操作成功", result["msg"])
	assert.Equal(t, testStudent.Name, result["data"].(map[string]interface{})["name"])
	assert.Equal(t, testStudent.Number, result["data"].(map[string]interface{})["number"])
	assert.Equal(t, testStudent.Age, result["data"].(map[string]interface{})["age"])
	assert.Equal(t, testStudent.Class, result["data"].(map[string]interface{})["class"])
	assert.Equal(t, testStudent.Sex, result["data"].(map[string]interface{})["sex"])
	// 验证学生是否已添加到全局变量中
	assert.NotNil(t, students[testStudent.Number])
	assert.Equal(t, testStudent.Name, students[testStudent.Number].Name)
	assert.Equal(t, testStudent.Number, students[testStudent.Number].Number)
	assert.Equal(t, testStudent.Age, students[testStudent.Number].Age)
	assert.Equal(t, testStudent.Scores, students[testStudent.Number].Scores)
	assert.Equal(t, testStudent.Class, students[testStudent.Number].Class)
	assert.Equal(t, testStudent.Sex, students[testStudent.Number].Sex)
}

func TestUpdateScore(t *testing.T) {
	// 创建一个默认的 GIN 引擎
	r := gin.Default()

	// 将处理器函数挂载到路由上（为了测试，我们直接挂载 UpdateScore）
	r.POST("/student/updateScore", addOrUpdateScore)

	// 准备测试数据
	testScores := map[string]int{"math": 95, "english": 88}
	testStudent := student{
		Name:   "John Doe",
		Age:    "20",
		Sex:    "Male",
		Class:  "Computer Science",
		Number: "12345",
		Scores: map[string]int{
			"math": 80,
		},
	}
	students[testStudent.Number] = &testStudent
	testScoresJSON, _ := json.Marshal(testScores)

	req, _ := http.NewRequest("POST", "/student/updateScore?number=12345", bytes.NewBuffer(testScoresJSON))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if response["msg"] != "操作成功" {
		t.Errorf("unexpected response message: got %v want %v",
			response["msg"], "操作成功")
	}
}

func TestDeleteStudent(t *testing.T) {
	r := gin.Default()
	r.DELETE("/students", deleteStudent) // 假设使用 DELETE 方法，根据实际情况调整

	// 测试删除一个存在的学生
	t.Run("delete existing student", func(t *testing.T) {
		studentNumber := "123456"
		students[studentNumber] = &student{
			Name:   "John Doe",
			Age:    "20",
			Sex:    "Male",
			Class:  "Computer Science",
			Number: studentNumber,
			Scores: map[string]int{
				"math": 80,
			},
		}
		req, _ := http.NewRequest("DELETE", "/students?number="+studentNumber, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"msg":"操作成功","data":" "}`, w.Body.String())

		// 验证学生是否已被删除
		mu.Lock()
		defer mu.Unlock()
		_, exists := students[studentNumber]
		assert.False(t, exists)
	})

	// 测试删除一个不存在的学生
	t.Run("delete non-existing student", func(t *testing.T) {
		nonExistentStudentNumber := "999999"
		req, _ := http.NewRequest("DELETE", "/students?number="+nonExistentStudentNumber, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.JSONEq(t, `{"error":"该学生不存在"}`, w.Body.String())
	})

	// 测试未提供学号的情况
	t.Run("delete student without number", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", "/students", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.JSONEq(t, `{"error":"未输入学号或学号为空"}`, w.Body.String())
	})
}

func TestDeleteScore(t *testing.T) {
	r := gin.Default()
	r.DELETE("/scores", deleteScore)

	// 设置一个测试学生
	testStudentNumber := "123456"
	testStudent := &student{
		Name:   "John Doe",
		Age:    "20",
		Sex:    "Male",
		Class:  "Computer Science",
		Number: testStudentNumber,
		Scores: map[string]int{"math": 80, "science": 90},
	}
	mu.Lock()
	students[testStudentNumber] = testStudent
	mu.Unlock()

	// 测试删除存在的成绩
	t.Run("delete existing scores", func(t *testing.T) {
		scoresToDelete := []string{"math"}
		scoresJSON, _ := json.Marshal(scoresToDelete)
		req, _ := http.NewRequest("DELETE", "/scores?number="+testStudentNumber, bytes.NewBuffer(scoresJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"code":200,"msg":"操作成功","data":" "}`, w.Body.String())
		mu.Lock()
		defer mu.Unlock()
		assert.True(t, students[testStudentNumber].Scores["math"] == 0)
	})

	// 测试删除不存在的成绩
	t.Run("delete non-existing score", func(t *testing.T) {
		scoresToDelete := []string{"english"}
		scoresJSON, _ := json.Marshal(scoresToDelete)
		req, _ := http.NewRequest("DELETE", "/scores?number="+testStudentNumber, bytes.NewBuffer(scoresJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.JSONEq(t, `{"error":"该学生不存在此科目的成绩","科目":"english"}`, w.Body.String())
	})

	// 测试删除成绩时学生不存在
	t.Run("delete scores for non-existing student", func(t *testing.T) {
		scoresToDelete := []string{"math"}
		scoresJSON, _ := json.Marshal(scoresToDelete)
		req, _ := http.NewRequest("DELETE", "/scores?number=999999", bytes.NewBuffer(scoresJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.JSONEq(t, `{"error":"学生不存在"}`, w.Body.String())
	})

	// 测试未提供学号的情况
	t.Run("delete scores without number", func(t *testing.T) {
		scoresToDelete := []string{"math"}
		scoresJSON, _ := json.Marshal(scoresToDelete)
		req, _ := http.NewRequest("DELETE", "/scores", bytes.NewBuffer(scoresJSON))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.JSONEq(t, `{"error":"未输入学号或学号为空"}`, w.Body.String())
	})
}
func TestUpdateStudent(t *testing.T) {
	// 初始化全局变量
	students["123456"] = &student{
		Number: "123456",
		Name:   "张三",
		Age:    "20",
		Sex:    "男",
		Class:  "一班",
		Scores: map[string]int{"数学": 90, "英语": 80},
	}

	// 创建一个模拟的 HTTP 请求和响应记录器
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PUT", "/update", nil)

	// 设置查询参数
	c.Request.URL.RawQuery = "number=123456"

	// 设置请求体（JSON 格式）
	updateJSON := `
	{
		"name": "李四",
		"age": "21",
		"sex": "男",
		"class": "二班",
		"score": {
			"数学": 95,
			"物理": 100
		}
	}`
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Body = io.NopCloser(strings.NewReader(updateJSON))

	// 调用被测函数（注意：确保 updateStudent 函数已经正确导入）
	updateStudent(c)

	// 验证响应状态码
	assert.Equal(t, http.StatusOK, w.Code)

	// 验证响应体（可以根据需要调整）
	var responseData map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&responseData)
	assert.NoError(t, err)
	assert.Equal(t, "操作成功", responseData["msg"])

	// 验证全局变量是否更新
	mu.Lock()
	defer mu.Unlock()
	updatedStudent, exists := students["123456"]
	assert.True(t, exists)
	assert.Equal(t, "李四", updatedStudent.Name)
	assert.Equal(t, "21", updatedStudent.Age)
	assert.Equal(t, "男", updatedStudent.Sex)
	assert.Equal(t, "二班", updatedStudent.Class)
	assert.Equal(t, 2, len(updatedStudent.Scores)) // 确保 Scores 字段被正确更新
	assert.Equal(t, 95, updatedStudent.Scores["数学"])
	assert.Equal(t, 100, updatedStudent.Scores["物理"])
}
func TestGetScore(t *testing.T) {
	// 初始化全局变量（如果测试需要的话）
	students = make(map[string]*student)
	students["12345"] = &student{
		Name:   "张三",
		Age:    "20",
		Sex:    "男",
		Class:  "一班",
		Number: "12345",
		Scores: map[string]int{
			"数学": 90,
			"英语": 85,
		},
	}

	// 设置一个默认的 Gin 引擎
	r := gin.Default()
	studentGroup := r.Group("/student")
	{
		studentGroup.GET("/getScore", getScore)
	}
	req, err := http.NewRequest("GET", "/student/getScore?number=12345&lessonName=数学", nil)
	require.NoError(t, err)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	expected := `{"code":200,"msg":"操作成功","data":90}`
	assert.JSONEq(t, expected, rr.Body.String())
}

func TestStudent(t *testing.T) {
	// 初始化全局变量
	students = make(map[string]*student)
	students["12345"] = &student{
		Name:   "张三",
		Age:    "20",
		Sex:    "男",
		Class:  "一班",
		Number: "12345",
		Scores: map[string]int{
			"数学": 90,
			"英语": 85,
		},
	}

	// 设置一个默认的 Gin 引擎
	r := gin.Default()
	studentGroup := r.Group("/student")
	{
		// 确保已经添加了 /studentInfo 路由
		studentGroup.GET("/studentInfo", getStudent)
	}

	// 创建一个请求
	req, err := http.NewRequest("GET", "/student/studentInfo?number=12345", nil)
	require.NoError(t, err)

	// 创建一个记录器来记录响应
	rr := httptest.NewRecorder()

	// 将请求路由到正确的处理器
	r.ServeHTTP(rr, req)

	// 检查状态码
	assert.Equal(t, http.StatusOK, rr.Code)

	// 定义期望的响应结构体
	var response struct {
		Code    int      `json:"code"`
		Msg     string   `json:"msg"`
		Student *student `json:"student"`
	}

	// 解析响应体到期望的结构体中
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)

	// 检查响应体内容
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "操作成功", response.Msg)
	assert.NotNil(t, response.Student)
	assert.Equal(t, "张三", response.Student.Name)
	assert.Equal(t, "20", response.Student.Age)
	assert.Equal(t, "男", response.Student.Sex)
	assert.Equal(t, "一班", response.Student.Class)
	assert.Equal(t, "12345", response.Student.Number)
	assert.Equal(t, 90, response.Student.Scores["数学"])
	assert.Equal(t, 85, response.Student.Scores["英语"])

	//测试错误情况
	t.Run("NotFound", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/student/studentInfo?number=99999", nil)
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
		var response struct {
			Message string `json:"message"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "该学生不存在", response.Message)
	})
}

func TestPostFile(t *testing.T) {
	r := gin.Default()
	r.POST("/csv/postFile", postFile)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	file, err := writer.CreateFormFile("file", "testfile.csv")
	require.NoError(t, err)
	_, err = file.Write([]byte("name,age,sex,class,number,\"{\"math\":100,\"english\":90}\"\nJohn,20,Male,A1,001,\"{\"math\":95,\"english\":85}\""))
	require.NoError(t, err)
	err = writer.Close()
	require.NoError(t, err)
	req, err := http.NewRequest("POST", "/csv/postFile", body)
	require.NoError(t, err)
	req.Header.Add("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"code":200,"msg":"上传成功","data":""}`, rec.Body.String())
	uploadDir := "./postFile"
	files, err := os.ReadDir(uploadDir)
	require.NoError(t, err)
	for _, file := range files {
		err = os.Remove(filepath.Join(uploadDir, file.Name()))
		require.NoError(t, err)
	}
	err = os.Remove(uploadDir)
	require.NoError(t, err)
}
