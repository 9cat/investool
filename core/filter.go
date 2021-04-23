// filter 对给定股票进行分析，筛出其中的优质公司。（好公司，但不代表当前股价在涨）
// 选股规则：
// 行业要分散
// 最新 ROE 高于 8%
// ROE 平均值小于 20 时，至少 3 年内逐年递增
// EPS 至少 3 年内逐年递增
// 营业总收入至少 3 年内逐年递增
// 净利润至少 3 年内逐年递增
// 估值较低或中等
// 股价低于合理价格
// 负债率低于 60%

package core

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/axiaoxin-com/logging"
	"github.com/axiaoxin-com/x-stock/datacenter"
	"github.com/axiaoxin-com/x-stock/datacenter/eastmoney"
	"github.com/axiaoxin-com/x-stock/model"
	"go.uber.org/zap"
)

// MaxWorkerCount 最大并发请求 worker 数
var MaxWorkerCount = 64

// DefaultFilter 默认过滤条件值
var DefaultFilter = eastmoney.Filter{
	MinROE:                   8.0,
	MinNetprofitYoyRatio:     0.0,
	MinToiYoyRatio:           0.0,
	MinZXGXL:                 0.0,
	MinNetprofitGrowthrate3Y: 0.0,
	MinIncomeGrowthrate3Y:    0.0,
	MinListingYieldYear:      0.0,
	MinPBNewMRQ:              0.0,
	MinPredictNetprofitRatio: 0.0,
	MinPredictIncomeRatio:    0.0,
	MinTotalMarketCap:        0.0,
	Industry:                 "",
	MinPrice:                 0.0,
	MaxPrice:                 0.0,
	ListingOver5Y:            false,
	ExcludeCYB:               true,
	ExcludeKCB:               true,
}

// AutoFilterStocks 按默认设置自动筛选股票
func AutoFilterStocks(ctx context.Context) (model.StockList, error) {
	return AutoFilterStocksWithFilter(ctx, DefaultFilter)
}

// AutoFilterStocksWithFilter 按设置自动筛选股票
func AutoFilterStocksWithFilter(ctx context.Context, filter eastmoney.Filter) (result model.StockList, err error) {
	stocks, err := datacenter.EastMoney.QuerySelectedStocksWithFilter(ctx, filter)
	if err != nil {
		return
	}
	logging.Infof(ctx, "AutoFilterStocksWithFilter will filter from %d stocks", len(stocks))

	// 最多 MaxWorkerCount 个 groutine 并发执行筛选任务
	workerCount := int(math.Min(float64(len(stocks)), float64(MaxWorkerCount)))
	jobChan := make(chan struct{}, workerCount)
	wg := sync.WaitGroup{}

	for _, baseInfo := range stocks {
		wg.Add(1)
		jobChan <- struct{}{}

		go func(ctx context.Context, baseInfo eastmoney.StockInfo) {
			defer func() {
				wg.Done()
				<-jobChan
				if r := recover(); r != nil {
					logging.Errorf(ctx, "recover from:%v", r)
				}
			}()
			// 按条件判断是否为优质股票
			stock, err := model.NewStock(ctx, baseInfo, false)
			if err != nil {
				logging.Error(ctx, "NewStock error:"+err.Error())
				return
			}

			checker := NewChecker(ctx, stock)
			if defects := checker.CheckFundamentals(ctx); len(defects) == 0 {
				result = append(result, stock)
			} else {
				logging.Info(ctx, fmt.Sprintf("%s %s has some defects", stock.BaseInfo.SecurityNameAbbr, stock.BaseInfo.Secucode), zap.Any("defects", defects))
			}
		}(ctx, baseInfo)
	}
	wg.Wait()
	logging.Infof(ctx, "AutoFilterStocksWithFilter selected %d stocks", len(result))
	result.SortByROE()
	return
}