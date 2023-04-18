#!/bin/sh

echo Resetting server... 
ssh -i "~/.ssh/personal.pem" ec2-user@ec2-3-145-54-79.us-east-2.compute.amazonaws.com 'sudo pkill server; sudo ~/go_pointing_poker/server &' 
# ssh -i "~/.ssh/personal.pem" ec2-user@ec2-3-145-54-79.us-east-2.compute.amazonaws.com 'sudo ~/go_pointing_poker/server'
