package subhd

import (
	"bytes"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/allanpk716/ChineseSubFinder/common"
	"github.com/allanpk716/ChineseSubFinder/sub_supplier"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/devices"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/nfnt/resize"
	"image/jpeg"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Supplier struct {
	reqParam common.ReqParam
	topic int
	rodlauncher *launcher.Launcher
}

func NewSupplier(_reqParam ... common.ReqParam) *Supplier {

	sup := Supplier{}
	sup.topic = common.DownloadSubsPerSite
	if len(_reqParam) > 0 {
		sup.reqParam = _reqParam[0]
		if sup.reqParam.Topic > 0 && sup.reqParam.Topic != sup.topic {
			sup.topic = sup.reqParam.Topic
		}
	}
	return &sup
}

func (s Supplier) GetSubListFromFile(filePath string) ([]sub_supplier.SubInfo, error) {
	/*
		虽然是传入视频文件路径，但是其实需要读取对应的视频文件目录下的
		movie.xml 以及 *.nfo，找到 IMDB id
		优先通过 IMDB id 去查找字幕
		如果找不到，再靠文件名提取影片名称去查找
	*/
	// 得到这个视频文件名中的信息
	info, err := common.GetVideoInfo(filePath)
	if err != nil {
		return nil, err
	}
	// 找到这个视频文件，然后读取它目录下的文件，尝试得到 IMDB ID
	fileRootDirPath := filepath.Dir(filePath)
	imdbId, err := common.GetImdbId(fileRootDirPath)
	if err != nil && err != common.CanNotFindIMDBID {
		return nil, err
	}

	var subInfoList []sub_supplier.SubInfo

	if imdbId != "" {
		// 先用 imdb id 找
		subInfoList, err = s.GetSubListFromKeyword(imdbId)
		if err != nil {
			return nil, err
		}
		// 如果有就优先返回
		if len(subInfoList) >0 {
			return subInfoList, nil
		}
	}

	// 如果没有，那么就用文件名查找
	subInfoList, err = s.GetSubListFromKeyword(info.Title)
	if err != nil {
		return nil, err
	}

	return subInfoList, nil
}

func (s Supplier) GetSubListFromKeyword(keyword string) ([]sub_supplier.SubInfo, error) {

	var subInfos  []sub_supplier.SubInfo
	detailPageUrl, err := s.Step0(keyword)
	if err != nil {
		return nil, err
	}
	subList, err := s.Step1(detailPageUrl)
	if err != nil {
		return nil, err
	}

	for _, item := range subList {
		hdContent, err := s.Step2(item.Url)
		if err != nil {
			return nil, err
		}

		var subInfo sub_supplier.SubInfo
		subInfo.Name = hdContent.Filename
		subInfo.Ext = hdContent.Ext
		subInfo.Language = common.ChineseSimple
		subInfo.Vote = 0
		subInfo.FileUrl = common.AddBaseUrl(common.SubSubHDRootUrl, item.Url)
		subInfo.Offset = 0
		subInfo.Data = hdContent.Data

		subInfos = append(subInfos, subInfo)
	}

	return subInfos, nil
}

// Step0 找到这个影片的详情列表
func (s Supplier) Step0(keyword string) (string, error) {

	result, err := s.httpGet(fmt.Sprintf(common.SubSubHDSearchUrl, url.QueryEscape(keyword)))
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`<a\shref="(/d/[\w]+)"><img`)
	matched := re.FindAllStringSubmatch(result, -1)
	if len(matched) < 1 || len(matched[0]) < 2{
		return "",  common.SubHDStep0HrefIsNull
	}
	return matched[0][1], nil
}
// Step1 获取影片的详情字幕列表
func (s Supplier) Step1(detailPageUrl string) ([]HdListItem, error) {
	detailPageUrl = common.AddBaseUrl(common.SubSubHDRootUrl, detailPageUrl)
	result, err := s.httpGet(detailPageUrl)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(result))
	if err != nil {
		return nil, err
	}
	var lists []HdListItem
	doc.Find(".table-sm tr").EachWithBreak(func(i int, tr *goquery.Selection) bool {
		if tr.Find("a.text-dark").Size() == 0 {
			return true
		}
		downUrl, exists := tr.Find("a.text-dark").Eq(0).Attr("href")
		if !exists {
			return true
		}
		title := strings.TrimSpace(tr.Find("a.text-dark").Text())

		downCount, err := common.GetNumber2int(tr.Find("td.p-3").Eq(1).Text())
		if err != nil {
			return true
		}

		ext := ""
		tr.Find(".text-secondary span").Each(func(a_i int, a_lb *goquery.Selection) {
			ext += a_lb.Text() + "，"
		})
		extLen := len(ext)
		if len(ext) > 0 {
			ext = ext[0 : extLen - 3]
		}

		authorInfo := tr.Find("a.text-dark").Eq(2).Text()

		rate := ""

		listItem := HdListItem{}
		listItem.Url = downUrl
		listItem.BaseUrl = common.SubSubHDRootUrl
		listItem.Title = title
		listItem.Ext = ext
		listItem.AuthorInfo = authorInfo
		listItem.Rate = rate
		listItem.DownCount = downCount

		if len(lists) > s.topic {
			return false
		}

		lists = append(lists, listItem)

		return true
	})

	return lists, nil
}
// Step2 下载字幕
func (s Supplier) Step2(subDownloadPageUrl string) (*HdContent, error) {
	subDownloadPageUrl = common.AddBaseUrl(common.SubSubHDRootUrl, subDownloadPageUrl)
	result, err := s.httpGet(subDownloadPageUrl)
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(result))
	if err != nil {
		return nil, err
	}
	// 是否有腾讯的防水墙
	matchList := doc.Find("#TencentCaptcha")
	if len(matchList.Nodes) < 1 {
		println("qiang")
	}
	//matchList = doc.Find("#down")
	//if len(matchList.Nodes) < 1 {
	//	println("not found down")
	//}
	postData := make(map[string]string)
	sid, exists := matchList.Attr("sid")
	if !exists {
		return nil, common.SubHDStep2SidIsNull
	}
	postData["sub_id"] = sid
	dToken, exists := matchList.Attr("dtoken1")
	if !exists {
		return nil, common.SubHDStep2DTokenIsNull
	}
	postData["dtoken1"] = dToken
	url2 := fmt.Sprintf("%s%s", common.SubSubHDRootUrl, "/ajax/down_ajax")
	result, err = s.httpPost(url2, postData, subDownloadPageUrl)
	if err != nil {
		return nil, err
	}
	if result == "" || strings.Contains(result, "true") == false {
		return nil, common.SubHDStep2ResultIsNullOrNotTrue
	}
	reg := regexp.MustCompile(`"url":"([^"]+)"`)
	arr := reg.FindStringSubmatch(result)
	if len(arr) == 0 {
		return nil, common.SubHDStep2PostResultGetUrlNotFound
	}
	downUrl := arr[1]
	downUrl = strings.ReplaceAll(downUrl, "\\", "")
	var filename = filepath.Base(downUrl)
	var data []byte
	data, filename, err = common.DownFile(downUrl, s.reqParam)
	if err != nil {
		return nil, err
	}
	return &HdContent{
		Filename: filename,
		Ext:      strings.ToLower(filepath.Ext(filename)),
		Data:     data,
	}, nil
}

func (s Supplier) httpGet(url string) (string, error) {
	s.reqParam.Referer = url
	httpClient := common.NewHttpClient(s.reqParam)
	resp, err := httpClient.R().Get(url)
	if err != nil {
		return "", err
	}
	//搜索验证 点击继续搜索
	if strings.Contains(resp.String(), "搜索验证") {
		println("搜索验证 reload", url)
		return s.httpGet(url)
	}
	return resp.String(), nil
}

func (s Supplier) httpPost(url string, postData map[string]string, referer string) (string, error) {

	s.reqParam.Referer = referer
	httpClient := common.NewHttpClient(s.reqParam)
	resp, err := httpClient.R().
		SetFormData(postData).
		Post(url)
	if err != nil {
		return "", err
	}
	return resp.String(), nil
}

// Simulation 模拟滑动过防水墙
func (s Supplier) Simulation() {
	// 感谢 https://www.bigs3.com/article/gorod-crack-slider-captcha/
	//設定設備參數
	screen := devices.Device{
		Title:          "Laptop with MDPI screen",
		Capabilities:   []string{"touch", "mobile"},
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36",
		Screen: devices.Screen{
			DevicePixelRatio: 1,
			Horizontal: devices.ScreenSize{
				Width:  1280,
				Height: 720,
			},
		},
	}
	//設定啓動器
	s.rodlauncher = launcher.New().
		Set("mute-audio").
		Set("default-browser-check").
		Set("disable-gpu").
		Set("disable-web-security").
		Set("no-sandbox").
		//關閉無頭模式，顯示瀏覽器窗體
		Delete("headless")

	//debug url
	launchers := s.rodlauncher.MustLaunch()
	fmt.Printf("debug url: %s\n", launchers)
	//連接到瀏覽器
	browser := rod.New().ControlURL(launchers).MustConnect()
	//新開一個Pages
	page := browser.DefaultDevice(screen).MustPage("")
	//跳轉到目標網域
	page.MustNavigate("https://007.qq.com/online.html").MustWaitLoad()

	// 切换到可疑用户
	page.MustElement("#app > section.wp-on-online > div > div > div > div.wp-on-box.col-md-5.col-md-offset-1 > div.wp-onb-tit > a:nth-child(2)").MustClick()
	//模擬Click點擊 "體驗驗證碼" 按鈕
	page.MustElement("#code").MustClick()
	//等待驗證碼窗體載入
	page.MustElement("#tcaptcha_iframe").MustWaitLoad()
	//進入到iframe
	iframe := page.MustElement("#tcaptcha_iframe").MustFrame()
	//等待拖動條加載, 延遲500秒檢測變化, 以確認加載完畢
	iframe.MustElement("#tcaptcha_drag_button").WaitStable(500 * time.Millisecond)
	//等待缺口圖像載入
	iframe.MustElement("#slideBg").MustWaitLoad()


	//取得帶缺口圖像
	shadowbg := iframe.MustElement("#slideBg").MustResource()
	//取得原始圖像
	src := iframe.MustElement("#slideBg").MustProperty("src")
	fullbg, fileName, err := common.DownFile(strings.Replace(src.String(), "img_index=1", "img_index=0", 1))
	if err != nil {
		return
	}
	println(fileName)
	//取得img展示的真實尺寸
	bgbox := iframe.MustElement("#slideBg").MustShape().Box()
	height, width := uint(math.Round(bgbox.Height)), uint(math.Round(bgbox.Width))
	//裁剪圖像
	shadowbg_img, _ := jpeg.Decode(bytes.NewReader(shadowbg))
	shadowbg_img = resize.Resize(width, height, shadowbg_img, resize.Lanczos3)
	fullbg_img, _ := jpeg.Decode(bytes.NewReader(fullbg))
	fullbg_img = resize.Resize(width, height, fullbg_img, resize.Lanczos3)

	//啓始left，排除干擾部份，所以右移10個像素
	left := fullbg_img.Bounds().Min.X + 10

	//啓始top, 排除干擾部份, 所以下移10個像素
	top := fullbg_img.Bounds().Min.Y + 10

	//最大left, 排除干擾部份, 所以左移10個像素
	maxleft := fullbg_img.Bounds().Max.X - 10

	//最大top, 排除干擾部份, 所以上移10個像素
	maxtop := fullbg_img.Bounds().Max.Y - 10

	//rgb比较阈值, 超出此阈值及代表找到缺口位置
	threshold := 20

	//缺口偏移, 拖動按鈕初始會偏移27.5
	distance := -27.5

	//取絕對值方法
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}
	search:
	for i := left; i <= maxleft; i++ {
		for j := top; j <= maxtop; j++ {
			color_a_R, color_a_G, color_a_B, _ := fullbg_img.At(i, j).RGBA()
			color_b_R, color_b_G, color_b_B, _ := shadowbg_img.At(i, j).RGBA()
			color_a_R, color_a_G, color_a_B = color_a_R >> 8, color_a_G >> 8, color_a_B >> 8
			color_b_R, color_b_G, color_b_B = color_b_R >> 8, color_b_G >> 8, color_b_B >> 8
			if abs(int(color_a_R) - int(color_b_R)) > threshold ||
				abs(int(color_a_G) - int(color_b_G)) > threshold ||
				abs(int(color_a_B) - int(color_b_B)) > threshold {
				distance += float64(i)
				fmt.Printf("info: 對比完畢, 偏移量: %v\n", distance)
				break search
			}
		}
	}

	//獲取拖動按鈕形狀
	dragbtnbox := iframe.MustElement("#tcaptcha_drag_thumb").MustShape().Box()
	//启用滑鼠功能
	mouse := page.Mouse
	//模擬滑鼠移動至拖動按鈕處, 右移3的原因: 拖動按鈕比滑塊圖大3個像素
	mouse.MustMove(dragbtnbox.X + 3, dragbtnbox.Y + (dragbtnbox.Height / 2))
	//按下滑鼠左鍵
	mouse.MustDown("left")
	//開始拖動
	mouse.Move(dragbtnbox.X + distance, dragbtnbox.Y + (dragbtnbox.Height / 2), 20)
	//鬆開滑鼠左鍵, 拖动完毕
	mouse.MustUp("left")
	//截圖保存
	page.MustScreenshot("result.png")
}

type HdListItem struct {
	Url        string `json:"url"`
	BaseUrl    string `json:"baseUrl"`
	Title      string `json:"title"`
	Ext        string `json:"ext"`
	AuthorInfo string `json:"authorInfo"`
	Lang       string `json:"lang"`
	Rate       string `json:"rate"`
	DownCount  int    `json:"downCount"`
}

type HdContent struct {
	Filename string `json:"filename"`
	Ext      string `json:"ext"`
	Data     []byte `json:"data"`
}