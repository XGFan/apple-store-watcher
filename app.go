package main

import (
	"apple-store-watcher/store"
	"flag"
	"fmt"
	"gopkg.in/yaml.v3"
	"log"
	"net/http"
	"os"
	"sort"
	"time"
)

type Rule struct {
	Store   string   `yaml:"store"`
	Sku     []string `yaml:"sku"`
	Trigger string   `yaml:"trigger"`
}

func prettify(m map[string]string) {
	result := make([]string, 0, len(m))
	for k, v := range m {
		result = append(result, fmt.Sprintf("%s - %s", v, k))
	}
	sort.Strings(result)
	for _, s := range result {
		fmt.Println(s)
	}
}

func main() {
	printStore := flag.Bool("store", false, "print store list")
	printProduct := flag.Bool("product", false, "print product list")
	config := flag.String("c", "config.yaml", "config file")
	verbose := flag.Bool("v", false, "verbose")
	flag.Parse()

	if *printStore {
		stores := store.GetStores()
		prettify(stores)
		return
	}

	if *printProduct {
		products := store.GetProducts()
		prettify(products)
		return
	}
	file, err := os.Open(*config)
	if err != nil {
		log.Fatal(err)
	}
	decoder := yaml.NewDecoder(file)
	rules := make([]Rule, 0)
	err = decoder.Decode(&rules)
	if err != nil {
		log.Fatal(err)
	}
	_ = file.Close()
	if *verbose {
		stores := store.GetStores()
		products := store.GetProducts()
		for _, rule := range rules {
			log.Printf("Store: %s", stores[rule.Store])
			for _, s := range rule.Sku {
				log.Printf("Product: %s", products[s])
			}
		}
	}

	for _, rule := range rules {
		go func(r Rule) {
			sleepTime := 5 * time.Second
			for {
				if store.Check(r.Store, r.Sku) {
					sleepTime = 2 * sleepTime
					log.Println("Send notification")
					SendNotification(r.Trigger)
				} else {
					sleepTime = 5 * time.Second
				}
				time.Sleep(sleepTime)
			}
		}(rule)
	}
	select {}
}

func SendNotification(u string) {
	_, _ = http.Post(u, "text/plain", nil)
}
