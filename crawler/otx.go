package crawler

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
	"ioc-provider/helper"
	"ioc-provider/helper/rabbit"
	"ioc-provider/model"
	"ioc-provider/repository"
	"log"
	"math"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Data struct {
	Results []Results `json:"results"`
	Count   int       `json:"count"`
}

type Results struct {
	ID                string       `json:"id"`
	Name              string       `json:"name"`
	Description       string       `json:"description"`
	AuthorName        string       `json:"author_name"`
	Modified          string       `json:"modified"`
	Created           string       `json:"created"`
	Indicators        []Indicators `json:"indicators"`
	Tags              []string     `json:"tags"`
	TargetedCountries []string     `json:"targeted_countries"`
	MalwareFamilies   []string     `json:"malware_families"`
	AttackIds         []string     `json:"attack_ids"`
	References        []string     `json:"references"`
	Industries        []string     `json:"industries"`
}

type Indicators struct {
	ID        int  `json:"id"`
	Indicator string `json:"indicator"`
	Type      string `json:"type"`
	Created   string `json:"created"`
}

func checkError(err error) {
	if err != nil {
		log.Println(err)
	}
}

//func getAllTimeModified() []int64 {
//	pathAPI := fmt.Sprintf("https://otx.alienvault.com/api/v1/pulses/subscribed?limit=50&modified_since=2019-01-01T00:00:00")
//	body, err := helper.HttpClient.GetOtxWithRetries(pathAPI)
//	checkError(err)
//	var data Data
//	json.Unmarshal(body, &data)
//	modifiedList := make([]int64, 0)
//	for _, item := range data.Results {
//		post := model.Post {
//			Modified:          item.Modified,
//		}
//		modifiedList = append(modifiedList, utcToTimestamp(post.Modified))
//	}
//	return modifiedList
//}
//
//func getLastTimeModified(modifiedList []int64) int64 {
//	max := modifiedList[0]
//	for _, value := range modifiedList {
//		if value > max {
//			max = value
//		}
//	}
//	return max
//}
//
//func utcToTimestamp(strTime string) int64 {
//	layout := "2006-01-02T15:04:05.000000"
//	t,_ := time.Parse(layout, strTime)
//	return t.Unix()
//}

func TotalPage() int {
	pathAPI := fmt.Sprintf("https://otx.alienvault.com/api/v1/pulses/subscribed?limit=50&modified_since=2019-01-01T00:00:00")
	body, err := helper.HttpClient.GetOtxWithRetries(pathAPI)
	checkError(err)
	var data Data
	json.Unmarshal(body, &data)
	countPost := data.Count
	totalPage := math.Ceil(float64(countPost) / float64(50))
	fmt.Println("totalPage->", int(totalPage))
	return int(totalPage)
}

func getDataOnePage(pathAPI string) ([]model.Post, []model.Indicators, error) {
	loc, _ := time.LoadLocation("Europe/London")
	postList := make([]model.Post, 0)
	iocList := make([]model.Indicators, 0)
	body, err := helper.HttpClient.GetOtxWithRetries(pathAPI)
	checkError(err)
	var data Data
	json.Unmarshal(body, &data)

	trustType := []string{"FileHash-MD5", "FileHash-PEHASH", "FileHash-SHA256", "FileHash-SHA1", "FileHash-IMPHASH", "FileHash-MD5", "URL", "URI", "hostname", "domain", "IPv6", "IPv4", "BitcoinAddress"}
	sample := []string{"FileHash-MD5", "FileHash-PEHASH", "FileHash-SHA256", "FileHash-SHA1", "FileHash-IMPHASH", "FileHash-MD5"}
	url := []string{"URL", "URI"}
	domain := []string{"hostname", "domain"}
	ipaddress := []string{"IPv6", "IPv4", "BitcoinAddress"}

	for _, item := range data.Results {
		post := model.Post{
			ID:                item.ID,
			Name:              item.Name,
			Description:       item.Description,
			AuthorName:        item.AuthorName,
			Modified:          item.Modified,
			Created:           item.Created,
			Tags:              item.Tags,
			TargetedCountries: item.TargetedCountries,
			MalwareFamilies:   item.MalwareFamilies,
			AttackIds:         item.AttackIds,
			Industries:        item.Industries,
			References:        item.References,
			CrawledTime:       strings.Replace(time.Now().In(loc).Format(time.RFC3339), "Z", "", -1),
		}
		postList = append(postList, post)

		for _, value := range item.Indicators {
			_, foundType := find(trustType, value.Type)
			if foundType {
				_, foundSample := find(sample, value.Type)
				if foundSample {
					value.Type = "sample"
				}

				_, foundUrl := find(url, value.Type)
				if foundUrl {
					value.Type = "url"
				}

				_, foundDomain := find(domain, value.Type)
				if foundDomain {
					value.Type = "domain"
				}

				_, foundIpaddress := find(ipaddress, value.Type)
				if foundIpaddress {
					value.Type = "ipaddress"
				}
				indicator := model.Indicators{
					IocID:       strconv.Itoa(value.ID),
					Ioc:         value.Indicator,
					IocType:     value.Type,
					CreatedTime: value.Created,
					CrawledTime: strings.Replace(time.Now().In(loc).Format(time.RFC3339), "Z", "", -1),
					Source:      "otx",
					Category:    item.Tags,
					PostID:      item.ID,
				}
				iocList = append(iocList, indicator)
			}
		}
	}
	return postList, iocList, nil
}

func Subscribed(repo repository.IocRepo) {
	sem := semaphore.NewWeighted(int64(25*runtime.NumCPU()))
	group, ctx := errgroup.WithContext(context.Background())

	totalPage := TotalPage()
	var totalPost int = 0
	var totalIoc int = 0

	if totalPage > 0 {
		for page := 1; page <= totalPage; page++ {
			pathAPI := fmt.Sprintf("https://otx.alienvault.com/api/v1/pulses/subscribed?limit=50&modified_since=2019-01-01T00:00:00&page=%d", page)

			err := sem.Acquire(ctx, 1)
			if err != nil {
				fmt.Printf("Acquire err = %+v\n", err)
				continue
			}
			group.Go(func() error {
				defer sem.Release(1)

				// do work
				postList, iocList, err := getDataOnePage(pathAPI)
				checkError(err)
				totalPost += len(postList)
				totalIoc += len(iocList)

				queue := helper.NewJobQueue(runtime.NumCPU())
				queue.Start()
				defer queue.Stop()

				queue.Submit(&OtxProcess{
					postList: postList,
					iocRepo: repo,
				})

				queue.Submit(&OtxProcess{
					iocList: iocList,
					iocRepo: repo,
				})

				return nil
			})
		}

		if err := group.Wait(); err != nil {
			fmt.Printf("g.Wait() err = %+v\n", err)
		}

		fmt.Println("done!")
	}
	fmt.Println("total post:", totalPost)
	fmt.Println("total ioc:", totalIoc)
}

func find(slice []string, val string) (int, bool) {
	for i, item := range slice {
		if item == val {
			return i, true
		}
	}
	return -1, false
}

type OtxProcess struct {
	postList     []model.Post
	iocList      []model.Indicators
	iocRepo  repository.IocRepo
}

func (process *OtxProcess) Process() {
	existsPost := process.iocRepo.ExistsIndex(model.IndexNamePost)
	if !existsPost {
		process.iocRepo.CreateIndex(model.IndexNamePost, model.MappingPost)
	}

	existsIoc := process.iocRepo.ExistsIndex(model.IndexNameIoc)
	if !existsIoc {
		process.iocRepo.CreateIndex(model.IndexNameIoc, model.MappingIoc)
	}

	existsIdPost := process.iocRepo.ExistsDocPost(model.IndexNamePost, process.postList)
	if !existsIdPost {
		success := process.iocRepo.InsertManyIndexPost(model.IndexNamePost, process.postList)
		if !success {
			return
		}
		rabbit.PublishPost("post", process.postList)
	}

	existsIdIoc := process.iocRepo.ExistsDocIoc(model.IndexNameIoc, process.iocList)
	if !existsIdIoc {
		success := process.iocRepo.InsertManyIndexIoc(model.IndexNameIoc, process.iocList)
		if !success {
			return
		}
		rabbit.PublishIoc("ioc", process.iocList)
	}
}