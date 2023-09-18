package store

import (
	_ "embed"
	"fmt"
	"github.com/tidwall/gjson"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
)

func GetStores() map[string]string {
	resp, err := http.DefaultClient.Get("https://www.apple.com/rsp-web/store-list?locale=zh_CN")

	if err != nil {
		return nil
	}
	all, err := io.ReadAll(resp.Body)
	results := make(map[string]string)
	for _, list := range gjson.GetBytes(all, "storeListData").Array() {
		if list.Get("locale").String() == "zh_CN" {
			for _, state := range list.Get("state").Array() {
				for _, store := range state.Get("store").Array() {
					name := fmt.Sprintf("%s - %s", store.Get("address.stateName"), store.Get("name"))
					code := store.Get("id").String()
					results[code] = name
				}
			}
		}
	}
	return results
}

func Check(store string, products []string) bool {
	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
		}
	}()

	var uri url.URL
	q := uri.Query()
	q.Set("little", "true")
	q.Set("mt", "regular")
	q.Set("store", store)

	for index, product := range products {
		q.Set("parts."+strconv.FormatInt(int64(index), 10), product)
	}
	queryStr := q.Encode()

	link := fmt.Sprintf(
		"https://www.apple.com.cn/shop/fulfillment-messages?%s",
		queryStr,
	)

	for _, v := range checkAvailability(link) {
		if v {
			return true
		}
	}
	return false
}

func checkAvailability(skUrl string) map[string]bool {
	availabilityMap := map[string]bool{}

	request, _ := http.NewRequest("GET", skUrl, nil)
	request.Header.Set("referer", "https://www.apple.com/shop/buy-iphone")
	request.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15")

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil
	}
	log.Println(resp.Status, skUrl)
	all, err := io.ReadAll(resp.Body)
	defer resp.Body.Close()
	gjson.GetBytes(all, "body.content.pickupMessage.stores").ForEach(func(key, value gjson.Result) bool {
		for productCode, availability := range value.Get("partsAvailability").Map() {
			availabilityMap[productCode] = availability.Get("messageTypes.compact.storeSelectionEnabled").Bool()
		}
		return true
	})
	log.Printf("result: %+v", availabilityMap)
	return availabilityMap
}
