using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Threading.Tasks;
using System.Web.Script.Serialization;
using NomadWiFi.UI.Models;

namespace NomadWiFi.UI.Services
{
    public class NomadCoreClient
    {
        private readonly string _coreExePath;
        private readonly JavaScriptSerializer _serializer = new JavaScriptSerializer();

        public NomadCoreClient()
        {
            var localBin = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.UserProfile), ".local", "bin", "nomadwifi.exe");
            var devBin = @"C:\code\nomadwifi\bin\nomadwifi.exe";
            var appBin = Path.Combine(AppDomain.CurrentDomain.BaseDirectory, "nomadwifi.exe");

            if (File.Exists(appBin))
                _coreExePath = appBin;
            else if (File.Exists(localBin))
                _coreExePath = localBin;
            else if (File.Exists(devBin))
                _coreExePath = devBin;
            else
                _coreExePath = "nomadwifi.exe";
        }

        private Task<string> RunCommandAsync(string arguments)
        {
            return Task.Run(() =>
            {
                try
                {
                    var psi = new ProcessStartInfo
                    {
                        FileName = _coreExePath,
                        Arguments = arguments,
                        RedirectStandardOutput = true,
                        RedirectStandardError = true,
                        UseShellExecute = false,
                        CreateNoWindow = true
                    };

                    using (var proc = Process.Start(psi))
                    {
                        if (proc == null) return string.Empty;
                        var output = proc.StandardOutput.ReadToEnd();
                        proc.WaitForExit(10000);
                        return output;
                    }
                }
                catch
                {
                    return string.Empty;
                }
            });
        }

        public async Task<InterfaceStatus> GetStatusAsync()
        {
            try
            {
                var json = await RunCommandAsync("status --json");
                if (string.IsNullOrWhiteSpace(json)) return null;
                return _serializer.Deserialize<InterfaceStatus>(json);
            }
            catch
            {
                return null;
            }
        }

        public async Task<List<AccessPoint>> ScanNetworksAsync()
        {
            try
            {
                var json = await RunCommandAsync("scan --json");
                if (string.IsNullOrWhiteSpace(json)) return new List<AccessPoint>();
                var list = _serializer.Deserialize<List<AccessPoint>>(json);
                return list ?? new List<AccessPoint>();
            }
            catch
            {
                return new List<AccessPoint>();
            }
        }

        public async Task<OptimizationResult> OptimizeAsync()
        {
            try
            {
                var json = await RunCommandAsync("optimize --json");
                if (string.IsNullOrWhiteSpace(json)) return null;
                return _serializer.Deserialize<OptimizationResult>(json);
            }
            catch
            {
                return null;
            }
        }

        public async Task<bool> ConnectAsync(string ssid)
        {
            try
            {
                var json = await RunCommandAsync(string.Format("connect \"{0}\" --json", ssid));
                return json != null && json.Contains("\"success\":true");
            }
            catch
            {
                return false;
            }
        }

        public async Task<bool> ConnectWithPasswordAsync(string ssid, string password)
        {
            try
            {
                var escapedPwd = password.Replace("\"", "\\\"");
                var json = await RunCommandAsync(string.Format("connect \"{0}\" --password \"{1}\" --json", ssid, escapedPwd));
                return json != null && json.Contains("\"success\":true");
            }
            catch
            {
                return false;
            }
        }
    }
}

