using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.IO;
using System.Threading;
using System.Threading.Tasks;
using System.Web.Script.Serialization;

namespace NomadWiFi.UI.Services
{
    /// <summary>
    /// Talks to the NomadWiFi core over a single long-lived process using
    /// newline-delimited JSON.
    ///
    /// The previous design launched the CLI once per query, which cost a
    /// process start for every refresh and gave the UI no way to learn about a
    /// dropped connection until its next poll. A persistent channel also lets
    /// the core push roaming events as they happen.
    /// </summary>
    public class AgentClient : IDisposable
    {
        private readonly JavaScriptSerializer _serializer = new JavaScriptSerializer();
        private readonly object _writeLock = new object();
        private readonly Dictionary<int, TaskCompletionSource<AgentResponse>> _pending =
            new Dictionary<int, TaskCompletionSource<AgentResponse>>();

        private Process _process;
        private int _nextId;
        private bool _disposed;

        private DateTime _startedUtc;
        private int _quickExits;

        // An older nomadwifi.exe on PATH does not understand "agent" and exits
        // straight away. Restarting it on every disconnect spawns processes in
        // a loop, so give up after a few immediate failures and let the UI say
        // so instead.
        private const int MaxQuickExits = 3;
        private static readonly TimeSpan QuickExitWindow = TimeSpan.FromSeconds(2);

        /// <summary>
        /// True once the core has repeatedly failed to stay alive, which means
        /// restarting it again is pointless.
        /// </summary>
        public bool GaveUp { get { return _quickExits >= MaxQuickExits; } }

        /// <summary>The core's last words on stderr, if it said anything.</summary>
        public string LastError { get; private set; }

        /// <summary>Raised when the core reports a state change or log line.</summary>
        public event Action<string, object> EventReceived;

        /// <summary>Raised when the core process stops unexpectedly.</summary>
        public event Action Disconnected;

        public string CorePath { get; private set; }
        public bool IsRunning { get { return _process != null && !_process.HasExited; } }

        public AgentClient()
        {
            CorePath = ResolveCorePath();
        }

        /// <summary>
        /// Environment variable naming the engine explicitly. Only honoured for
        /// development: it points the app at a core outside its install tree.
        /// </summary>
        private const string CoreOverrideVariable = "NOMADWIFI_CORE";

        /// <summary>
        /// Finds the core executable, which must live inside the install tree.
        ///
        /// Everything here is deliberately restrictive. The engine is spawned as
        /// a child process, so anywhere it can be picked up from is somewhere an
        /// executable can be substituted -- and the earlier candidate list
        /// included a user-writable directory plus a bare-name lookup that fell
        /// through to the current working directory and PATH. That is how a
        /// pre-rewrite binary got run and reported a live connection as "not
        /// connected". Only siblings of this executable are trusted now.
        ///
        /// The GUI and the CLI are both named nomadwifi.exe, so every candidate
        /// is checked against this process's own path: without that guard the
        /// app would launch itself in a loop.
        /// </summary>
        private static string ResolveCorePath()
        {
            string self = null;
            try
            {
                self = Process.GetCurrentProcess().MainModule.FileName;
            }
            catch
            {
                // Not fatal: the candidates below still resolve, we only lose
                // the self-reference check.
            }

            var baseDir = AppDomain.CurrentDomain.BaseDirectory;
            var candidates = new List<string>();

            // Development escape hatch, checked first so a dev tree can point at
            // a core built elsewhere. Never set in a shipped install.
            var overridePath = Environment.GetEnvironmentVariable(CoreOverrideVariable);
            if (!string.IsNullOrWhiteSpace(overridePath))
            {
                candidates.Add(overridePath);
            }

            candidates.Add(Path.Combine(baseDir, "core", "nomadwifi.exe"));
            candidates.Add(Path.Combine(baseDir, "nomadwifi-core.exe"));

            foreach (var candidate in candidates)
            {
                string full;
                try { full = Path.GetFullPath(candidate); }
                catch { continue; }

                if (!File.Exists(full)) continue;
                if (self != null && string.Equals(full, self, StringComparison.OrdinalIgnoreCase))
                {
                    continue; // this candidate is the GUI itself
                }
                return full;
            }

            // No fallback to PATH. Returning the expected location means a
            // missing engine surfaces as "engine not running" naming the path
            // it wanted, rather than silently running whatever was found.
            return Path.Combine(baseDir, "core", "nomadwifi.exe");
        }

        public bool Start()
        {
            if (IsRunning) return true;
            if (GaveUp) return false;

            try
            {
                var psi = new ProcessStartInfo
                {
                    FileName = CorePath,
                    Arguments = "agent",
                    RedirectStandardInput = true,
                    RedirectStandardOutput = true,
                    RedirectStandardError = true,
                    UseShellExecute = false,
                    CreateNoWindow = true,
                    // Pin the working directory to the engine's own folder.
                    // Left unset it inherits the shortcut's "Start in", which
                    // leaks into relative path resolution inside the core.
                    WorkingDirectory = Path.GetDirectoryName(CorePath) ?? string.Empty,
                };

                _startedUtc = DateTime.UtcNow;
                _process = Process.Start(psi);
                if (_process == null) return false;

                var reader = new Thread(ReadLoop) { IsBackground = true };
                reader.Start();

                // Keep whatever the core complains about; when it refuses to
                // run, that line is the only useful diagnostic we get.
                var errors = new Thread(ReadErrorLoop) { IsBackground = true };
                errors.Start();
                return true;
            }
            catch
            {
                _process = null;
                return false;
            }
        }

        private void ReadLoop()
        {
            try
            {
                string line;
                while ((line = _process.StandardOutput.ReadLine()) != null)
                {
                    if (line.Length == 0) continue;
                    try { Dispatch(line); }
                    catch { /* one malformed line must not kill the channel */ }
                }
            }
            catch
            {
                // The process ended or the pipe broke; handled below.
            }

            // A core that dies almost immediately is not a crash to recover
            // from, it is the wrong executable. Count those separately.
            if (DateTime.UtcNow - _startedUtc < QuickExitWindow) _quickExits++;
            else _quickExits = 0;

            FailAllPending("the NomadWiFi core stopped responding");
            var handler = Disconnected;
            if (handler != null && !_disposed) handler();
        }

        private void ReadErrorLoop()
        {
            try
            {
                string line;
                while ((line = _process.StandardError.ReadLine()) != null)
                {
                    if (line.Trim().Length > 0) LastError = line.Trim();
                }
            }
            catch
            {
                // The pipe closed with the process; nothing to do.
            }
        }

        private void Dispatch(string line)
        {
            var response = _serializer.Deserialize<AgentResponse>(line);
            if (response == null) return;

            if (!string.IsNullOrEmpty(response.@event))
            {
                var handler = EventReceived;
                if (handler != null) handler(response.@event, response.result);
                return;
            }

            TaskCompletionSource<AgentResponse> pending;
            lock (_pending)
            {
                if (!_pending.TryGetValue(response.id, out pending)) return;
                _pending.Remove(response.id);
            }
            pending.TrySetResult(response);
        }

        private void FailAllPending(string reason)
        {
            List<TaskCompletionSource<AgentResponse>> waiting;
            lock (_pending)
            {
                waiting = new List<TaskCompletionSource<AgentResponse>>(_pending.Values);
                _pending.Clear();
            }
            foreach (var tcs in waiting)
            {
                tcs.TrySetResult(new AgentResponse { ok = false, error = reason });
            }
        }

        /// <summary>Sends a request and awaits its reply.</summary>
        public async Task<AgentResponse> CallAsync(string method, object parameters, int timeoutMs)
        {
            if (!IsRunning && !Start())
            {
                return new AgentResponse { ok = false, error = "the NomadWiFi core could not be started" };
            }

            int id = Interlocked.Increment(ref _nextId);
            var tcs = new TaskCompletionSource<AgentResponse>();
            lock (_pending) { _pending[id] = tcs; }

            var payload = new Dictionary<string, object> { { "id", id }, { "method", method } };
            if (parameters != null) payload["params"] = parameters;

            try
            {
                lock (_writeLock)
                {
                    _process.StandardInput.WriteLine(_serializer.Serialize(payload));
                    _process.StandardInput.Flush();
                }
            }
            catch (Exception ex)
            {
                lock (_pending) { _pending.Remove(id); }
                return new AgentResponse { ok = false, error = ex.Message };
            }

            var completed = await Task.WhenAny(tcs.Task, Task.Delay(timeoutMs)).ConfigureAwait(false);
            if (completed != tcs.Task)
            {
                lock (_pending) { _pending.Remove(id); }
                return new AgentResponse { ok = false, error = method + " timed out" };
            }
            return tcs.Task.Result;
        }

        public Task<AgentResponse> CallAsync(string method)
        {
            return CallAsync(method, null, 120000);
        }

        /// <summary>Sends a request and converts its result to a typed value.</summary>
        public async Task<T> CallAsync<T>(string method, object parameters, int timeoutMs) where T : class
        {
            var response = await CallAsync(method, parameters, timeoutMs).ConfigureAwait(false);
            if (response == null || !response.ok || response.result == null) return null;

            try
            {
                return _serializer.ConvertToType<T>(response.result);
            }
            catch
            {
                return null;
            }
        }

        public Task<T> CallAsync<T>(string method) where T : class
        {
            return CallAsync<T>(method, null, 120000);
        }

        public void Dispose()
        {
            _disposed = true;
            try
            {
                if (IsRunning)
                {
                    _process.StandardInput.Close();
                    if (!_process.WaitForExit(2000)) _process.Kill();
                }
            }
            catch { }
        }
    }

    public class AgentResponse
    {
        public int id { get; set; }
        public bool ok { get; set; }
        public object result { get; set; }
        public string error { get; set; }

        /// <summary>Named "event" on the wire; "event" is a C# keyword.</summary>
        public string @event { get; set; }
    }
}
