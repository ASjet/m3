package mod

import (
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

var (
	rowConfig = table.RowConfig{
		AutoMerge: true,
	}
)

func renderModInfoTable(modInfoMap fetchModResult, directFileMap, depFileMap fetchFileResult) string {
	t := table.NewWriter()
	t.SetStyle(table.StyleRounded)
	t.Style().Format.Header = text.FormatDefault

	index := 1
	appendMod := func(fileMap fetchFileResult, isDep bool) {
		for modID, result := range fileMap {
			info := modInfoMap[modID]
			if info.Err != nil {
				errMsg := info.Err.Error()
				t.AppendRow(table.Row{index, modID, errMsg, errMsg, errMsg, isDep}, rowConfig)
			} else {
				mod := info.Value
				date := "⛔No release found!⛔"
				file := result.Value
				if file != nil {
					date = file.FileDate.Format(time.RFC3339)
				}
				t.AppendRow(table.Row{
					index,
					modID,
					mod.Name,
					date,
					isDep,
				}, rowConfig)
			}
			index++
		}
	}

	t.AppendHeader(table.Row{"#", "ModID", "Name", "Latest Release Date", "Indirect"})
	appendMod(depFileMap, true)
	appendMod(directFileMap, false)
	return t.Render()
}
