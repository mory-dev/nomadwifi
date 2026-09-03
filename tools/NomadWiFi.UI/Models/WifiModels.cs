using System;
using System.Collections.Generic;

namespace NomadWiFi.UI.Models
{
    public class InterfaceStatus
    {
        public string interface_name { get; set; }
        public string description { get; set; }
        public bool connected { get; set; }
        public string ssid { get; set; }
        public string bssid { get; set; }
        public string band { get; set; }
        public int channel { get; set; }
        public string radio_type { get; set; }
        public int rx_mbps { get; set; }
        public int tx_mbps { get; set; }
        public int signal_percent { get; set; }
        public string gateway_ip { get; set; }
        public double gateway_latency_ms { get; set; }
        public double packet_loss_percent { get; set; }
        public bool captive_portal { get; set; }
    }

    public class AccessPoint
    {
        public string ssid { get; set; }
        public string bssid { get; set; }
        public int signal_percent { get; set; }
        public string band { get; set; }
        public int channel { get; set; }
        public string radio_type { get; set; }
        public string authentication { get; set; }
        public string cipher { get; set; }
        public double quality_score { get; set; }

        public bool Is5GHz
        {
            get { return band != null && (band.Contains("5") || band.Contains("6")); }
        }

        public string ScoreDisplay
        {
            get { return quality_score.ToString("F0"); }
        }

        public string BandTag
        {
            get { return Is5GHz ? "5 GHz" : "2.4 GHz"; }
        }

        public string SignalDisplay
        {
            get { return string.Format("{0}%", signal_percent); }
        }
    }

    public class OptimizationResult
    {
        public bool success { get; set; }
        public bool switched { get; set; }
        public string current_ssid { get; set; }
        public string target_ssid { get; set; }
        public string band { get; set; }
        public double score { get; set; }
        public string reason { get; set; }
        public string error { get; set; }
    }
}
