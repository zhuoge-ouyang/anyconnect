using System;
using System.Drawing;
using System.IO;
using System.Reflection;
using System.Windows.Forms;

namespace SplitTunnel.UI {
    internal sealed class ContactForm : Surface {
        internal ContactForm(string root):base("联系作者",new Size(460,390),root){
            FormBorderStyle=FormBorderStyle.FixedDialog;MaximizeBox=false;MinimizeBox=false;StartPosition=FormStartPosition.CenterParent;
            var info=Place(this,new Panel{Dock=DockStyle.Fill},0,0,460,390);
            var qr=Place(this,new Panel{Dock=DockStyle.Fill,Visible=false},0,0,460,390);
            Label(info,24,22,412,38,"联系作者",18,true);Label(info,24,70,412,30,"有问题、建议或需要定制，可以联系作者。",10,false,Muted);
            Label(info,24,120,412,28,"作者：卓哥",11,true);Label(info,24,160,412,28,"微信号：ai_creater99",11,true);Label(info,24,204,412,28,"需要安卓客户端请联系作者。",10,true,Amber);
            Button(info,24,314,120,42,"复制微信号",delegate{Clipboard.SetText("ai_creater99");},true);
            Button(info,154,314,164,42,"查看微信二维码",delegate{info.Visible=false;qr.Visible=true;});Button(info,328,314,108,42,"关闭",delegate{Close();});
            Label(qr,24,16,412,36,"微信二维码",17,true).TextAlign=ContentAlignment.MiddleCenter;Label(qr,24,52,412,24,"微信扫码添加作者",10,false,Muted).TextAlign=ContentAlignment.MiddleCenter;
            Picture(qr,"wechat-contact-qr.png",115,78,230,230);Button(qr,115,326,108,42,"返回",delegate{qr.Visible=false;info.Visible=true;});Button(qr,237,326,108,42,"关闭",delegate{Close();});
        }
    }
    internal sealed class RechargeForm : Surface {
        internal RechargeForm(string root):base("账号充值说明",new Size(664,611),root){
            FormBorderStyle=FormBorderStyle.FixedDialog;MaximizeBox=false;MinimizeBox=false;StartPosition=FormStartPosition.CenterParent;
            Label(this,24,18,610,34,"线上购买与充值流程",16,true);
            string text;using(var stream=Assembly.GetExecutingAssembly().GetManifestResourceStream("Recharge.txt"))using(var reader=new StreamReader(stream))text=reader.ReadToEnd();
            Place(this,new TextBox{Multiline=true,ScrollBars=ScrollBars.Vertical,ReadOnly=true,Text=text.Replace("\r\n","\n").Replace("\n",Environment.NewLine),BackColor=Color.FromArgb(255,255,249),Font=UiFont(10)},24,62,616,476);
            Button(this,24,556,126,38,"复制注册链接",delegate{Clipboard.SetText("https://vip90123.com/signup");});Button(this,160,556,126,38,"复制推荐码",delegate{Clipboard.SetText("Sm3xWXkUif");});Button(this,514,556,126,38,"知道了",delegate{Close();},true);
        }
    }
}
