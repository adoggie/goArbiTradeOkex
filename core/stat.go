package core

import (
	"fmt"
	"github.com/adoggie/jaguar/utils"
	"github.com/rodaine/table"
	"io"
)

func PrintTask(task *OrderTask, writer io.Writer) {
	fmt.Println()
	_, _ = fmt.Fprintln(writer, "\nTask:", task.Request.Id(), " Start:",
		utils.FormatTimeYmdHms(task.Start), " End:", utils.FormatTimeYmdHms(task.End))

	tbl := table.New("Name", "BS", "Target", "Present", "Ask0", "Bid0", "Px", "Size",
		"ClOrdId", "State", "FillSz", "ACCFillSz", "Fee", "UTime")
	if writer != nil {
		tbl.WithWriter(writer)
	}

	for _, slice := range task.orderSlices {
		for _, order := range slice.orderReturns {
			//text := fmt.Sprintf("Name:%s\n")
			if order.Inner.Orders[0].FillSz <= 0 {
				//continue
			}
			tbl.AddRow(slice.PlaceOrder.InstID,
				utils.If(slice.BuySell == Buy, "buy", "sell"),
				slice.Target,
				slice.Present,
				slice.OrderBook.Books[0].Asks[0].DepthPrice,
				slice.OrderBook.Books[0].Bids[0].DepthPrice,
				slice.PlaceOrder.Px,
				slice.PlaceOrder.Sz,
				order.Inner.Orders[0].ClOrdID,
				order.Inner.Orders[0].State,
				order.Inner.Orders[0].FillSz,
				order.Inner.Orders[0].AccFillSz,
				order.Inner.Orders[0].Fee,
				order.Inner.Orders[0].UTime,
			)
		}
	}

	tbl.Print()
	fmt.Println()

}
