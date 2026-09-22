using System;
using System.Collections.Generic;
using System.Diagnostics;
using System.Drawing;
using System.Globalization;
using System.Linq;
using System.Net.NetworkInformation;
using System.Windows.Forms;
using System.Windows.Forms.DataVisualization.Charting;

namespace SplitTunnel.UI {
    internal sealed class TrafficCounter {
        internal string Id, Name;
        internal long Received, Sent;
    }

    internal sealed class TrafficSample {
        internal double Time, Download, Upload;
    }

    // Read exactly the saved pre-VPN uplink. Never add a tunnel and its underlying NIC together.
    internal static class TrafficReader {
        internal static TrafficCounter Read(int index) {
            if(index<=0)throw new InvalidOperationException("尚未识别联网网卡");
            foreach(var nic in NetworkInterface.GetAllNetworkInterfaces()) {
                var props=nic.GetIPProperties();
                bool match=nic.Supports(NetworkInterfaceComponent.IPv4) && props.GetIPv4Properties().Index==index;
                if(!match && nic.Supports(NetworkInterfaceComponent.IPv6))match=props.GetIPv6Properties().Index==index;
                if(!match)continue;
                if(nic.OperationalStatus!=OperationalStatus.Up || nic.NetworkInterfaceType==NetworkInterfaceType.Loopback || nic.NetworkInterfaceType==NetworkInterfaceType.Tunnel)
                    throw new InvalidOperationException("联网网卡不可用");
                var stats=nic.GetIPStatistics();
                return new TrafficCounter {Id=nic.Id,Name=nic.Name,Received=stats.BytesReceived,Sent=stats.BytesSent};
            }
            throw new InvalidOperationException("联网网卡已断开或发生变化");
        }
    }

    internal sealed class TrafficSampler {
        private TrafficCounter previous;
        private double previousTime;
        internal readonly List<TrafficSample> Samples=new List<TrafficSample>();
        internal string Message="等待采样", InterfaceName="未识别";
        internal double Download,Upload;
        internal bool Available;
        internal void Reset(string reason) { previous=null;Available=false;Download=Upload=0;Samples.Clear();Message=reason;InterfaceName="未识别"; }
        internal void Accept(TrafficCounter counter,double now) {
            double elapsed=now-previousTime;
            if(previous==null || previous.Id!=counter.Id || elapsed<=0 || elapsed>5 || counter.Received<previous.Received || counter.Sent<previous.Sent) {
                Samples.Clear();Available=false;Download=Upload=0;Message="正在采样…";
            } else {
                Download=(counter.Received-previous.Received)/elapsed;Upload=(counter.Sent-previous.Sent)/elapsed;
                Available=true;Message="每秒更新 · 最近 60 秒";
                Samples.Add(new TrafficSample {Time=now,Download=Download,Upload=Upload});
                Samples.RemoveAll(p=>p.Time<now-60);
                while(Samples.Count>61)Samples.RemoveAt(0);
            }
            previous=counter;previousTime=now;InterfaceName=counter.Name;
        }
        internal static string Format(double bytes) {
            if(bytes>=1000000000)return (bytes/1000000000).ToString("0.00",CultureInfo.InvariantCulture)+" GB/s";
            if(bytes>=1000000)return (bytes/1000000).ToString("0.00",CultureInfo.InvariantCulture)+" MB/s";
            if(bytes>=1000)return (bytes/1000).ToString("0.0",CultureInfo.InvariantCulture)+" KB/s";
            return Math.Max(0,bytes).ToString("0",CultureInfo.InvariantCulture)+" B/s";
        }
    }

    internal sealed class TrafficPanel : Panel {
        internal static readonly Color Tint=Color.FromArgb(242,249,244);
        internal readonly TrafficSampler Sampler=new TrafficSampler();
        internal readonly Label Download,Upload,Notice,Interface;
        private readonly Label downloadUnit,uploadUnit,downBefore,downNow,upBefore,upNow;
        internal readonly Chart DownChart,UpChart;
        internal Func<int,TrafficCounter> ReadCounter=TrafficReader.Read;
        private readonly Stopwatch clock=Stopwatch.StartNew();
        private int lastIndex;
        private readonly Label title,downTitle,upTitle,scope,footnote;
        internal TrafficPanel() {
            BackColor=Tint;
            title=Surface.Label(this,28,28,230,40,"实时网速",21,true);
            downTitle=Surface.Label(this,28,88,230,28,"下载速度",12,true,Color.FromArgb(0,147,139));
            Download=Surface.Label(this,28,126,245,54,"—",29,true);
            downloadUnit=Surface.Label(this,170,164,100,34,"",13,false,Surface.Muted);
            DownChart=MakeChart(Color.FromArgb(0,166,156));Controls.Add(DownChart);
            upTitle=Surface.Label(this,28,302,230,28,"上传速度",12,true,Color.FromArgb(0,133,190));
            Upload=Surface.Label(this,28,340,245,54,"—",29,true);
            uploadUnit=Surface.Label(this,170,398,100,34,"",13,false,Surface.Muted);
            UpChart=MakeChart(Color.FromArgb(0,148,214));Controls.Add(UpChart);
            downBefore=Surface.Label(this,28,300,100,24,"60秒前",9,false,Surface.Muted);downNow=Surface.Label(this,180,300,80,24,"现在",9,false,Surface.Muted);downNow.TextAlign=ContentAlignment.TopRight;
            upBefore=Surface.Label(this,28,536,100,24,"60秒前",9,false,Surface.Muted);upNow=Surface.Label(this,180,536,80,24,"现在",9,false,Surface.Muted);upNow.TextAlign=ContentAlignment.TopRight;
            scope=Surface.Label(this,28,510,245,44,"本机网络 · 当前联网网卡",10,true);
            Interface=Surface.Label(this,28,548,245,42,"未识别",9,false,Surface.Muted);
            Notice=Surface.Label(this,28,590,245,26,"等待采样",9,false,Surface.Muted);
            footnote=Surface.Label(this,28,620,245,60,"含直连与 VPN 流量\n实时速率，不是带宽测速",9,false,Surface.Muted);
        }
        private static Chart MakeChart(Color color) {
            var chart=new Chart {BackColor=Tint,AntiAliasing=AntiAliasingStyles.All,AccessibleName="最近 60 秒传输速率"};
            var area=new ChartArea("traffic") {BackColor=Tint,Position=new ElementPosition(0,0,100,100),InnerPlotPosition=new ElementPosition(4,8,92,68)};
            area.AxisX.Minimum=-60;area.AxisX.Maximum=0;area.AxisX.Interval=60;area.AxisX.MajorGrid.Enabled=false;area.AxisX.MajorTickMark.Enabled=false;
            area.AxisX.LabelStyle.Enabled=false;area.AxisX.LineColor=Color.FromArgb(201,222,214);
            area.AxisX.CustomLabels.Add(new CustomLabel(-65,-55,"60秒前",0,LabelMarkStyle.None));
            area.AxisY.Minimum=0;area.AxisY.MajorGrid.Enabled=false;area.AxisY.MajorTickMark.Enabled=false;area.AxisY.LabelStyle.Enabled=false;area.AxisY.LineColor=Color.FromArgb(201,222,214);
            chart.ChartAreas.Add(area);
            chart.Series.Add(new Series("fill") {ChartType=SeriesChartType.Area,Color=Color.FromArgb(35,color),BorderWidth=0});
            chart.Series.Add(new Series("rate") {ChartType=SeriesChartType.Line,Color=color,BorderWidth=2});
            return chart;
        }
        internal void LayoutAt(float dpi) {
            int pad=(int)(28*dpi),w=Width-2*pad;
            title.SetBounds(pad,(int)(70*dpi),w,(int)(42*dpi));
            downTitle.SetBounds(pad,(int)(126*dpi),w,(int)(28*dpi));Download.SetBounds(pad,(int)(162*dpi),w,(int)(62*dpi));
            DownChart.SetBounds(pad,(int)(220*dpi),w,(int)(105*dpi));
            upTitle.SetBounds(pad,(int)(350*dpi),w,(int)(28*dpi));Upload.SetBounds(pad,(int)(386*dpi),w,(int)(62*dpi));
            UpChart.SetBounds(pad,(int)(444*dpi),w,(int)(105*dpi));
            downBefore.SetBounds(pad,(int)(305*dpi),w/2,(int)(24*dpi));downNow.SetBounds(pad+w/2,(int)(305*dpi),w/2,(int)(24*dpi));
            upBefore.SetBounds(pad,(int)(529*dpi),w/2,(int)(24*dpi));upNow.SetBounds(pad+w/2,(int)(529*dpi),w/2,(int)(24*dpi));
            foreach(var label in new[]{downBefore,downNow,upBefore,upNow}){label.BackColor=Tint;label.BringToFront();}
            scope.SetBounds(pad,(int)(576*dpi),w,(int)(26*dpi));Interface.SetBounds(pad,(int)(606*dpi),w,(int)(32*dpi));
            Notice.SetBounds(pad,(int)(642*dpi),w,(int)(26*dpi));footnote.SetBounds(pad,(int)(668*dpi),w,(int)(42*dpi));
            PlaceUnit(Download,downloadUnit,dpi);PlaceUnit(Upload,uploadUnit,dpi);
            AutoScrollMinSize=new Size(0,(int)(710*dpi));AutoScroll=true;
        }
        internal void Tick(int index) {
            if(index!=lastIndex){Sampler.Reset("正在识别联网网卡…");lastIndex=index;}
            try {Sampler.Accept(ReadCounter(index),clock.Elapsed.TotalSeconds);}
            catch(NetworkInformationException){Sampler.Reset("暂不可用：无法读取网卡统计");}
            catch(InvalidOperationException ex){Sampler.Reset("暂不可用："+ex.Message);}
            catch(System.Net.Sockets.SocketException){Sampler.Reset("暂不可用：网络接口已变化");}
            RenderSample();
        }
        internal void Unavailable(){Sampler.Reset("暂不可用：连接状态不可读");RenderSample();}
        internal void RenderSample() {
            SetValue(Download,downloadUnit,Sampler.Download);SetValue(Upload,uploadUnit,Sampler.Upload);
            Interface.Text=Sampler.InterfaceName;Notice.Text=Sampler.Message;
            Plot(DownChart,true);Plot(UpChart,false);
        }
        private void SetValue(Label value,Label unit,double rate){string[] parts=TrafficSampler.Format(rate).Split(' ');value.Text=Sampler.Available?parts[0]:"—";unit.Text=Sampler.Available?parts[1]:"";using(var g=CreateGraphics())PlaceUnit(value,unit,g.DpiX/96f);}
        private static void PlaceUnit(Label value,Label unit,float dpi){int text=TextRenderer.MeasureText(value.Text,value.Font).Width;unit.SetBounds(value.Left+text,value.Top+(int)(24*dpi),(int)(80*dpi),(int)(28*dpi));unit.BringToFront();}
        private void Plot(Chart chart,bool down) {
            foreach(var series in chart.Series)series.Points.Clear();
            double latest=Sampler.Samples.Count==0?0:Sampler.Samples.Last().Time;
            foreach(var p in Sampler.Samples)foreach(var series in chart.Series)series.Points.AddXY(p.Time-latest,down?p.Download:p.Upload);
            chart.ChartAreas[0].AxisY.Maximum=Math.Max(1000,Sampler.Samples.Count==0?1000:Sampler.Samples.Max(p=>down?p.Download:p.Upload)*1.15);
        }
    }
}
