package mod

import (
	"fmt"
	"maps"
	"sync"
	"sync/atomic"

	"github.com/ASjet/go-curseforge"
	"github.com/ASjet/go-curseforge/api"
	"github.com/ASjet/go-curseforge/schema"
	"github.com/ASjet/go-curseforge/schema/enum"
	"github.com/ASjet/m3/internal/index"
	"github.com/ASjet/m3/internal/util"
	"github.com/pkg/errors"
	"github.com/vbauerster/mpb"
	"github.com/vbauerster/mpb/decor"
)

func Init(apiKey string) {
	curseforge.InitDefault(apiKey)
}

func Add(modLoaderStr string, confirm, optDep bool, ids ...int) error {
	modLoader, err := enum.ParseModLoader(modLoaderStr)
	if err != nil {
		return errors.Wrapf(err, "invalid mod loader %q", modLoaderStr)
	}
	modIDs := util.Map(func(id int) schema.ModID { return schema.ModID(id) }, ids...)
	proc := mpb.New()

	fileSpn := proc.AddSpinner(2,
		mpb.SpinnerOnLeft,
		mpb.BarClearOnComplete(),
		mpb.PrependDecorators(
			decor.OnComplete(decor.Name("🔥", decor.WCSyncSpaceR), "✅"),
			decor.OnComplete(decor.Name("Resolving dependency", decor.WCSyncSpaceR), "Resolve dependency"),
			decor.OnComplete(decor.Percentage(decor.WCSyncSpace), ""),
		),
	)

	// Fetch direct mod files
	modFileMap := fetchModFiles(modLoader, modIDs...)
	fileSpn.Increment()

	// Fetch dependencies files
	depIDs := util.Filter(func(id schema.ModID) bool {
		_, ok := modFileMap[id]
		return !ok
	}, extractDeps(optDep, modFileMap)...)
	depFileMap := fetchModFiles(modLoader, depIDs...)
	fileSpn.Increment()

	allModIDs := append(util.Keys(modFileMap), util.Keys(depFileMap)...)
	infoSpn := proc.AddSpinner(1,
		mpb.SpinnerOnLeft,
		mpb.BarClearOnComplete(),
		mpb.PrependDecorators(
			decor.OnComplete(decor.Name("🔥", decor.WCSyncSpaceR), "✅"),
			decor.OnComplete(decor.Name("Fetching mods info", decor.WCSyncSpaceR), "Fetch mods info"),
			decor.OnComplete(decor.Percentage(decor.WCSyncSpace), ""),
		),
	)
	// Fetch mod info
	modMap, found := fetchMods(allModIDs...)
	infoSpn.Increment()
	proc.Wait()

	// Print mod info table
	fmt.Println(renderModInfoTable(modMap, modFileMap, depFileMap))

	// Prompt user for download confirmation with mod info
	if found > 0 && promptDownload(confirm) {
		maps.Copy(modFileMap, depFileMap)
		downloadMods := make([]*util.DownloadTask, 0, len(modFileMap))

		// Write to index
		for modID, result := range modFileMap {
			mod := index.NewMod(modLoader, modMap[modID].Value, result.Value)
			index.Mods[modID] = mod
			if file := result.Value; file != nil && file.DownloadURL != "" && file.FileName != "" {
				downloadMods = append(downloadMods, &util.DownloadTask{
					FileName: mod.File.Name,
					Url:      mod.File.DownloadUrl,
					MD5Sum:   mod.File.HashMD5,
				})
			}
		}

		downloadCnt := util.Download(downloadMods...)
		fmt.Printf("(%d/%d) mod downloaded\n", downloadCnt, len(allModIDs))
	}
	return nil
}

type fetchModResult map[schema.ModID]util.Result[*schema.Mod]

func fetchMods(modIDs ...schema.ModID) (fetchModResult, int64) {
	successCnt := new(atomic.Int64)
	wg, mu := new(sync.WaitGroup), new(sync.Mutex)
	result := make(fetchModResult, len(modIDs))

	wg.Add(len(modIDs))
	for _, id := range modIDs {
		go func(modID schema.ModID) {
			defer wg.Done()
			resp, err := api.Mod(modID)

			var res util.Result[*schema.Mod]
			if err != nil {
				res = util.Err[*schema.Mod](err)
				res.Value = &schema.Mod{ID: modID}
			} else {
				res = util.Ok(&resp.Data)
				successCnt.Add(1)
			}

			mu.Lock()
			result[modID] = res
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	return result, successCnt.Load()
}

type fetchFileResult map[schema.ModID]util.Result[*schema.File]

func fetchModFiles(modLoader enum.ModLoader, modIDs ...schema.ModID) fetchFileResult {
	wg, mu := new(sync.WaitGroup), new(sync.Mutex)
	result := make(fetchFileResult, len(modIDs))

	wg.Add(len(modIDs))
	for _, id := range modIDs {
		go func(modID schema.ModID) {
			defer wg.Done()
			resp, err := api.ModFiles(modID,
				api.ModFiles.WithGameVersion(index.Meta.GameVersion),
				api.ModFiles.WithModLoader(modLoader),
				api.ModFiles.WithIndex(0),
				api.ModFiles.WithPageSize(1),
			)
			if err == nil && len(resp.Data) == 0 {
				err = errors.Errorf("mod %d has no files for game version %s and mod loader %s",
					modID, index.Meta.GameVersion, modLoader)
			}

			var res util.Result[*schema.File]
			if err != nil {
				res = util.Err[*schema.File](err)
			} else {
				res = util.Ok(&resp.Data[0])
			}

			mu.Lock()
			result[modID] = res
			mu.Unlock()
		}(id)
	}
	wg.Wait()

	return result
}

func extractDeps(option bool, results fetchFileResult) []schema.ModID {
	dt := NewDepTree[schema.ModID]()
	for modID, result := range results {
		if result.Value == nil {
			continue
		}
		dt.AddNode(Dep(modID, util.Map(
			func(dep schema.FileDependency) schema.ModID { return dep.ModID },
			util.Filter(
				func(dep schema.FileDependency) bool {
					switch dep.RelationType {
					case enum.RequiredDependency:
						return true
					case enum.OptionalDependency:
						return option
					default:
						return false
					}
				},
				result.Value.Dependencies...,
			)...,
		)...))
	}
	return dt.TopSort()
}
