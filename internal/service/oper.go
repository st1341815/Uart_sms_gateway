package service

// OperData 数据来源 https://mcc-mnc.net/
var OperData = map[string]string{
	"46000": "中国移动",
	"46001": "中国联通",
	"46002": "中国移动",
	"46003": "中国电信",
	"46004": "中国移动",
	"46005": "中国电信",
	"46006": "中国联通",
	"46007": "中国移动",
	"46008": "中国移动",
	"46011": "中国电信",
	"46015": "中国广电",
	"23410": "O2 - United Kingdom",
	"45403": "3 HK",
	"45404": "3 HK",
	"45419": "PCCW HK",
	"45416": "PCCW HK",
	"45410": "CSL HK",
	"45412": "CMHK",
	"45406": "SmarTone HK",
	"45400": "CSL HK",
}

// operatorFromIMSI 由 IMSI 前 5 位（MCC+MNC）推断运营商名称。
//
// 抽成公共函数是因为有两处要用：状态帧处理（device 常态运行）与
// 模组探测（L1，探测未配置的卡）。两处若各自维护一份映射，
// 早晚会出现「界面显示中国移动、探测结果却显示 46000」这种自相矛盾。
//
// 返回空串表示 IMSI 不可用；映射表里没有的 PLMN 原样返回（比空着强，
// 用户至少能看到 460xx 这类线索）。
func operatorFromIMSI(imsi string) string {
	if len(imsi) < 5 {
		return ""
	}
	plmn := imsi[:5]
	if v, ok := OperData[plmn]; ok {
		return v
	}
	return plmn
}
