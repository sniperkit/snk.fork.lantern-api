/*
Sniperkit-Bot
- Status: analyzed
*/

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/SimonBackx/lantern-crawler/queries"
	"github.com/gorilla/mux"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type AggregatedResult struct {
	Id        string    `json:"_id" bson:"_id"`
	LastFound time.Time `json:"lastFound" bson:"lastFound"`
	Count     int       `json:"count" bson:"count"`
}

/**
 * /results/{queryId}[?host=...&category=...&nogrouping]
 */
func resultsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	queryId, found := vars["queryId"]
	if !found {
		internalErrorHandler(w, r, fmt.Errorf("queryId not set"))
		return
	}

	queryIdBson := bson.ObjectIdHex(queryId)
	c := mongo.DB("lantern").C("results")

	queryValues := r.URL.Query()
	hostArr, found := queryValues["host"]
	categoryArr, useCategory := queryValues["category"]
	if len(categoryArr) == 0 {
		useCategory = false
	}

	if !found || len(hostArr) == 0 {
		matching := bson.M{"queryId": queryIdBson}
		if useCategory {
			matching = bson.M{"queryId": queryIdBson, "category": categoryArr[0]}
		}

		_, nogrouping := queryValues["nogrouping"]

		if nogrouping {
			var result []queries.Result
			err := c.Find(matching).Select(bson.M{"_id": 1, "queryId": 1, "lastFound": 1, "createdOn": 1, "occurrences": 1, "url": 1, "snippet": 1, "title": 1, "host": 1, "category": 1}).Sort("-lastFound").Limit(200).All(&result)
			if err != nil {
				internalErrorHandler(w, r, err)
				return
			}

			jsonValue, err := json.Marshal(result)
			if err != nil {
				internalErrorHandler(w, r, err)
				return
			}

			fmt.Fprintf(w, "%s", jsonValue)
			return
		}

		// Accumuleren
		pipe := c.Pipe(
			[]bson.M{
				{"$match": matching},
				{"$group": bson.M{"_id": "$host", "count": bson.M{"$sum": 1}, "lastFound": bson.M{"$max": "$lastFound"}}},
				{"$sort": bson.M{"lastFound": -1}},
				{"$limit": 100},
			})
		iter := pipe.Iter()

		var result []AggregatedResult
		err := iter.All(&result)
		if err != nil {
			internalErrorHandler(w, r, err)
			return
		}

		jsonValue, err := json.Marshal(result)
		if err != nil {
			internalErrorHandler(w, r, err)
			return
		}

		resultCount := 0
		for _, val := range result {
			resultCount += val.Count
		}
		SetResultCount(queryIdBson, resultCount)

		fmt.Fprintf(w, "%s", jsonValue)
		return
	}
	host := hostArr[0]

	matching := bson.M{"queryId": queryIdBson, "host": host}
	if useCategory {
		matching = bson.M{"queryId": queryIdBson, "host": host, "category": categoryArr[0]}
	}

	// Specifieke host
	var result []queries.Result
	err := c.Find(matching).Select(bson.M{"_id": 1, "queryId": 1, "lastFound": 1, "createdOn": 1, "occurrences": 1, "url": 1, "snippet": 1, "title": 1, "host": 1, "category": 1}).Sort("-lastFound").Limit(200).All(&result)
	if err != nil {
		internalErrorHandler(w, r, err)
		return
	}

	jsonValue, err := json.Marshal(result)
	if err != nil {
		internalErrorHandler(w, r, err)
		return
	}

	fmt.Fprintf(w, "%s", jsonValue)
}

/**
 * /result/{id}
 */
func resultHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	id, found := vars["id"]
	if !found {
		internalErrorHandler(w, r, fmt.Errorf("id not set"))
		return
	}

	idBson := bson.ObjectIdHex(id)

	c := mongo.DB("lantern").C("results")
	var result queries.Result
	err := c.FindId(idBson).One(&result)
	if err != nil {
		if err == mgo.ErrNotFound {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Invalid id.")
		} else {
			internalErrorHandler(w, r, err)
		}

		return
	}

	jsonValue, err := json.Marshal(result)
	if err != nil {
		internalErrorHandler(w, r, err)
		return
	}

	fmt.Fprintf(w, "%s", jsonValue)
}

/**
 * DELETE /result/{id}
 */
func deleteResultHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	id, found := vars["id"]
	if !found {
		internalErrorHandler(w, r, fmt.Errorf("id not set"))
		return
	}

	idBson := bson.ObjectIdHex(id)

	c := mongo.DB("lantern").C("results")
	var result queries.Result
	err := c.FindId(idBson).One(&result)
	if err != nil {
		if err == mgo.ErrNotFound {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Invalid id.")
		} else {
			internalErrorHandler(w, r, err)
		}

		return
	}

	err = c.RemoveId(idBson)
	if err != nil {
		internalErrorHandler(w, r, err)
		return
	}
	DecreaseResultCount(result.QueryId, 1)

	fmt.Fprintf(w, "ok")
}

/**
 * POST /result/{id}/set-category
 */
func setResultCategoryHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	id, found := vars["id"]
	if !found {
		internalErrorHandler(w, r, fmt.Errorf("id not set"))
		return
	}
	idBson := bson.ObjectIdHex(id)

	category, err := ioutil.ReadAll(r.Body)
	if err != nil {
		internalErrorHandler(w, r, err)
		return
	}

	c := mongo.DB("lantern").C("results")

	var result queries.Result
	err = c.FindId(idBson).One(&result)

	if err != nil {
		if err == mgo.ErrNotFound {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Invalid id.")
		} else {
			internalErrorHandler(w, r, err)
		}

		return
	}

	err = c.UpdateId(result.Id, bson.M{"$set": bson.M{"category": string(category)}})

	if err == nil {
		fmt.Fprintf(w, "Success")
		return
	} else {
		internalErrorHandler(w, r, err)
	}
}

/**
 * /result
 */
func newResultHandler(w http.ResponseWriter, r *http.Request) {
	str, err := ioutil.ReadAll(r.Body)

	if err != nil {
		internalErrorHandler(w, r, err)
		return
	}

	var result queries.Result
	err = json.Unmarshal(str, &result)

	if err != nil || result.Url == nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Invalid result.")
		return
	}

	// todo: verify

	c := mongo.DB("lantern").C("results")
	if result.Id == "" {
		fmt.Println("New result?")

		// Eerst kijken of deze URL + host niet al bestaat
		var foundResult queries.Result
		err := c.Find(bson.M{"queryId": result.QueryId, "host": result.Host, "snippet": result.Snippet}).One(&foundResult)

		if err != nil {
			fmt.Println("New unique snippet for this query")

			err = c.Insert(result)

			if err != nil {
				internalErrorHandler(w, r, err)
				return
			}

			IncreaseResultCount(result.QueryId)

		} else {
			result.Id = foundResult.Id
			fmt.Println("Already found this snippet for this query and host")

			// Onaanpasbare velden
			result.Category = foundResult.Category
			result.CreatedOn = foundResult.CreatedOn

			if result.Url == foundResult.Url {
				// Content enzo mag aangepast orden
				result.Urls = foundResult.Urls
			} else {
				// Content mag niet aangepast worden
				result.Body = foundResult.Body

				alreadyInUrls := false
				for _, u := range foundResult.Urls {
					if u == *result.Url {
						alreadyInUrls = true
						break
					}
				}
				if !alreadyInUrls && len(foundResult.Urls) < 10 {
					result.Urls = append(foundResult.Urls, *result.Url)
				} else {
					result.Urls = foundResult.Urls
					// eigenlijk geen update meer nodig nu...? -> minder belasting op mongodb
					fmt.Fprintf(w, "Success")
					return
				}

				result.Url = foundResult.Url
				result.Title = foundResult.Title
			}

			result.Occurrences = foundResult.Occurrences + 1

			err = c.UpdateId(result.Id, result)
			if err != nil {
				internalErrorHandler(w, r, err)
				return
			}
		}

	} else {
		fmt.Printf("Update result / _id = %v\n", result.Id)

		err = c.UpdateId(result.Id, result)
		if err != nil {
			internalErrorHandler(w, r, err)
			return
		}
	}

	fmt.Fprintf(w, "Success")
}

/**
 * DELETE /results/{queryid}[?host=...]
 */
func deleteResultsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	queryId, found := vars["queryId"]
	if !found {
		internalErrorHandler(w, r, fmt.Errorf("queryId not set"))
		return
	}

	queryIdBson := bson.ObjectIdHex(queryId)
	queryValues := r.URL.Query()
	hostArr, found := queryValues["host"]
	var q bson.M
	if !found || len(hostArr) == 0 {
		q = bson.M{"queryId": queryIdBson}
	} else {
		q = bson.M{"queryId": queryIdBson, "host": hostArr[0]}
	}

	// results deleten
	resultsCollection := mongo.DB("lantern").C("results")
	info, err := resultsCollection.RemoveAll(q)
	if err != nil {
		internalErrorHandler(w, r, err)
		return
	}
	DecreaseResultCount(queryIdBson, info.Removed)

	fmt.Fprintf(w, "ok")
}
