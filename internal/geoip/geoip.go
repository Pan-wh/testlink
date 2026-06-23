package geoip

import (
	"fmt"
	"net"
	"strings"

	"github.com/IncSW/geoip2"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"

	"testlink/internal/model"
)

type Service struct {
	v4         *xdb.Searcher
	v6         *xdb.Searcher
	countryRdr *geoip2.CountryReader
	asnRdr     *geoip2.ASNReader
}

func New(v4Path, v6Path, countryMMDB, asnMMDB string) (*Service, error) {
	v4, err := newSearcher(v4Path)
	if err != nil {
		return nil, fmt.Errorf("load ip2region v4: %w", err)
	}
	var v6 *xdb.Searcher
	if v6Path != "" {
		v6, err = newSearcher(v6Path)
		if err != nil {
			return nil, fmt.Errorf("load ip2region v6: %w", err)
		}
	}
	s := &Service{v4: v4, v6: v6}
	if countryMMDB != "" {
		s.countryRdr, err = geoip2.NewCountryReaderFromFile(countryMMDB)
		if err != nil {
			return nil, fmt.Errorf("load maxmind country: %w", err)
		}
	}
	if asnMMDB != "" {
		s.asnRdr, err = geoip2.NewASNReaderFromFile(asnMMDB)
		if err != nil {
			return nil, fmt.Errorf("load maxmind asn: %w", err)
		}
	}
	return s, nil
}

func newSearcher(dbPath string) (*xdb.Searcher, error) {
	cBuff, err := xdb.LoadContentFromFile(dbPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	header, err := xdb.LoadHeaderFromBuff(cBuff)
	if err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	version, err := xdb.VersionFromHeader(header)
	if err != nil {
		return nil, fmt.Errorf("version: %w", err)
	}
	return xdb.NewWithBuffer(version, cBuff)
}

func (s *Service) Lookup(ip string) model.GeoInfo {
	if ip == "" || ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "127.") ||
		strings.HasPrefix(ip, "10.") || strings.HasPrefix(ip, "192.168.") {
		return model.GeoInfo{Country: "本地"}
	}

	// Try ip2region first (best for mainland China province/city/ISP detail)
	searcher := s.v4
	if strings.Contains(ip, ":") && s.v6 != nil {
		searcher = s.v6
	}
	result, err := searcher.Search(ip)
	ip2regionGI := model.GeoInfo{}
	if err == nil && result != "" {
		ip2regionGI = parse(result)
	}

	// Always query MaxMind for cross-validation
	parsed := net.ParseIP(ip)
	mmGI := model.GeoInfo{}
	if parsed != nil && s.countryRdr != nil {
		cr, err := s.countryRdr.Lookup(parsed)
		if err == nil && cr != nil {
			if name, ok := cr.Country.Names["zh-CN"]; ok {
				mmGI.Country = name
			} else if name, ok := cr.Country.Names["en"]; ok {
				mmGI.Country = name
			}
		}
	}
	if parsed != nil && s.asnRdr != nil {
		ar, err := s.asnRdr.Lookup(parsed)
		if err == nil && ar != nil {
			mmGI.ASN = fmt.Sprintf("%d", ar.AutonomousSystemNumber)
			mmGI.ISP = ar.AutonomousSystemOrganization
		}
	}

	// Merge: prefer ip2region for mainland China detail, MaxMind for country correction
	if ip2regionGI.Country == "中国" || ip2regionGI.Country == "China" {
		if mmGI.Country != "" && mmGI.Country != "中国" && mmGI.Country != "China" {
			// ip2region misclassified — Taiwan/HK/Macau → use MaxMind country, keep ip2region ISP
			mmGI.Province = ip2regionGI.Province
			mmGI.City = ip2regionGI.City
			if mmGI.ISP == "" {
				mmGI.ISP = ip2regionGI.ISP
			}
			if mmGI.Country == "" {
				mmGI.Country = "未知"
			}
			return mmGI
		}
		// Mainland China — trust ip2region (better province/city/ISP)
		if ip2regionGI.Country != "" {
			return ip2regionGI
		}
	}

	// Not China or ip2region failed — prefer MaxMind for international
	if mmGI.Country != "" {
		return mmGI
	}
	if ip2regionGI.Country != "" {
		return ip2regionGI
	}
	return model.GeoInfo{Country: "未知"}
}

// ip2region format: 国家|区域|省份|城市|ISP
func parse(raw string) model.GeoInfo {
	parts := strings.Split(raw, "|")
	g := model.GeoInfo{}
	if len(parts) >= 1 && parts[0] != "0" {
		g.Country = parts[0]
	}
	if len(parts) >= 3 && parts[2] != "0" {
		g.Province = parts[2]
	}
	if len(parts) >= 4 && parts[3] != "0" {
		g.City = parts[3]
	}
	if len(parts) >= 5 && parts[4] != "0" {
		g.ISP = parts[4]
	}
	if g.Country == "" {
		g.Country = "未知"
	}
	return g
}

func (s *Service) Close() {
	if s.v4 != nil {
		s.v4.Close()
	}
	if s.v6 != nil {
		s.v6.Close()
	}
}
