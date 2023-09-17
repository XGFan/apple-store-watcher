package store

import (
	_ "embed"
	"fmt"
	"github.com/tidwall/gjson"
)

//go:embed assets/14.json
var iphone14Json string

//go:embed assets/15.json
var iphone15Json string

//go:embed assets/15pro.json
var iphone15proJson string

func parseProductsJson(context string) map[string]string {
	products := gjson.Get(context, "products")
	results := make(map[string]string)
	products.ForEach(func(key, value gjson.Result) bool {
		partNumber := value.Get("partNumber")
		familyType := value.Get("familyType")
		storage := value.Get("dimensionCapacity")
		colorCode := value.Get("dimensionColor")
		colorPath := fmt.Sprintf("displayValues.dimensionColor.%s.value", colorCode)
		color := gjson.Get(context, colorPath)
		results[partNumber.String()] = fmt.Sprintf("%s - %s - %s", familyType, color, storage)
		return true
	})
	return results
}

func parseProductsJsons(contexts []string) map[string]string {
	results := make(map[string]string)
	for _, context := range contexts {
		for k, v := range parseProductsJson(context) {
			results[k] = v
		}
	}
	return results
}

func GetProducts() map[string]string {
	return parseProductsJsons([]string{iphone14Json, iphone15Json, iphone15proJson})
}
